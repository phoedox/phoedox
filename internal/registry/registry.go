// Package registry asks container registries for the current digest of an
// image tag (OCI distribution API, anonymous token auth). The instance does
// this, never the agent: agents only talk to their instance.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Ref is a parsed image reference.
type Ref struct {
	Registry string // host, e.g. registry-1.docker.io, ghcr.io
	Repo     string // e.g. library/nginx
	Tag      string
}

// ErrPinned is returned for images referenced by digest: they never "update".
var ErrPinned = errors.New("registry: image is pinned to a digest")

// Parse reads an image reference like docker would ("nginx", "nginx:1.27",
// "ghcr.io/org/app:v2", "localhost:5000/app").
func Parse(image string) (Ref, error) {
	image = strings.TrimSpace(image)
	if image == "" || strings.ContainsAny(image, " \t\n") {
		return Ref{}, fmt.Errorf("registry: invalid image %q", image)
	}
	if strings.Contains(image, "@") {
		return Ref{}, ErrPinned
	}
	r := Ref{Registry: "registry-1.docker.io", Tag: "latest"}
	name := image
	if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		r.Tag, name = name[i+1:], name[:i]
	}
	if first, rest, ok := strings.Cut(name, "/"); ok && (strings.ContainsAny(first, ".:") || first == "localhost") {
		r.Registry, name = first, rest
	}
	if r.Registry == "docker.io" || r.Registry == "index.docker.io" {
		r.Registry = "registry-1.docker.io"
	}
	if r.Registry == "registry-1.docker.io" && !strings.Contains(name, "/") {
		name = "library/" + name
	}
	if name == "" || r.Tag == "" {
		return Ref{}, fmt.Errorf("registry: invalid image %q", image)
	}
	r.Repo = name
	return r, nil
}

var accept = strings.Join([]string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.docker.distribution.manifest.v2+json",
	"application/vnd.oci.image.manifest.v1+json",
}, ", ")

// Digest returns the digest the registry serves for the tag right now.
func Digest(ctx context.Context, client *http.Client, r Ref) (string, error) {
	u := "https://" + r.Registry + "/v2/" + r.Repo + "/manifests/" + url.PathEscape(r.Tag)
	res, err := head(ctx, client, u, "")
	if err != nil {
		return "", err
	}
	if res.StatusCode == http.StatusUnauthorized {
		token, err := anonymousToken(ctx, client, res.Header.Get("WWW-Authenticate"))
		if err != nil {
			return "", err
		}
		if res, err = head(ctx, client, u, token); err != nil {
			return "", err
		}
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry: %s answered %s", r.Registry, res.Status)
	}
	d := res.Header.Get("Docker-Content-Digest")
	if !strings.HasPrefix(d, "sha256:") {
		return "", fmt.Errorf("registry: %s sent no digest", r.Registry)
	}
	return d, nil
}

func head(ctx context.Context, client *http.Client, u, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	res.Body.Close()
	return res, nil
}

// anonymousToken follows a Bearer challenge (realm, service, scope).
func anonymousToken(ctx context.Context, client *http.Client, challenge string) (string, error) {
	scheme, params, ok := strings.Cut(challenge, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", errors.New("registry: needs credentials")
	}
	p := map[string]string{}
	for _, part := range splitParams(params) {
		k, v, _ := strings.Cut(part, "=")
		p[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	realm, err := url.Parse(p["realm"])
	if err != nil || realm.Scheme != "https" {
		return "", errors.New("registry: invalid token realm")
	}
	q := realm.Query()
	for _, k := range []string{"service", "scope"} {
		if p[k] != "" {
			q.Set(k, p[k])
		}
	}
	realm.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry: token %s", res.Status)
	}
	var t struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&t); err != nil {
		return "", err
	}
	if t.Token == "" {
		t.Token = t.AccessToken
	}
	if t.Token == "" {
		return "", errors.New("registry: no token")
	}
	return t.Token, nil
}

// splitParams splits a,b="c,d" at commas outside quotes.
func splitParams(s string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			cur.WriteRune(r)
		case r == ',' && !quoted:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(out, cur.String())
}
