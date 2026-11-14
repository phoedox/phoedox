package core

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Agent self-updates: the instance proxies agent releases so agents only
// ever talk to their instance. The update job (signed in the browser like
// every agent action) carries the download path and the SHA-256 from the
// release's SHA256SUMS; the agent installs exactly that file.

var (
	releaseTagRe   = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	releaseAssetRe = regexp.MustCompile(`^phoedox-agent-(linux|windows|darwin|freebsd)-(amd64|arm64)(\.exe)?$`)
)

// AgentReleasePath is where agents download a release asset from the instance.
func AgentReleasePath(tag, asset string) string {
	return "/api/v1/agent/releases/" + tag + "/" + asset
}

type releaseCache struct {
	mu      sync.Mutex
	latest  string
	checked time.Time
	sums    map[string]map[string]string // tag → asset → sha256
}

func (s *Service) releaseClient() *http.Client {
	c := *s.RequestPolicy.Client()
	c.Timeout = 5 * time.Minute
	return &c
}

// latestAgentRelease returns the newest release tag (cached for an hour).
func (s *Service) latestAgentRelease(ctx context.Context) (string, error) {
	s.releases.mu.Lock()
	defer s.releases.mu.Unlock()
	if s.releases.latest != "" && time.Since(s.releases.checked) < time.Hour {
		return s.releases.latest, nil
	}
	if s.AgentReleases == "" {
		return "", errors.New("agent releases are not configured")
	}
	// GitHub redirects …/releases/latest to …/releases/tag/<tag>.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.AgentReleases+"/latest", nil)
	if err != nil {
		return "", err
	}
	c := s.releaseClient()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := c.Do(req)
	if err != nil {
		return "", err
	}
	res.Body.Close()
	tag := path.Base(res.Header.Get("Location"))
	if !releaseTagRe.MatchString(tag) {
		return "", fmt.Errorf("no agent release found (%s)", res.Status)
	}
	s.releases.latest, s.releases.checked = tag, time.Now()
	return tag, nil
}

// releaseSums reads SHA256SUMS of a release (cached).
func (s *Service) releaseSums(ctx context.Context, tag string) (map[string]string, error) {
	s.releases.mu.Lock()
	if m, ok := s.releases.sums[tag]; ok {
		s.releases.mu.Unlock()
		return m, nil
	}
	s.releases.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.AgentReleases+"/download/"+tag+"/SHA256SUMS", nil)
	if err != nil {
		return nil, err
	}
	res, err := s.releaseClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SHA256SUMS of %s: %s", tag, res.Status)
	}
	m := map[string]string{}
	sc := bufio.NewScanner(io.LimitReader(res.Body, 64<<10))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && len(f[0]) == 64 {
			m[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	s.releases.mu.Lock()
	if s.releases.sums == nil {
		s.releases.sums = map[string]map[string]string{}
	}
	s.releases.sums[tag] = m
	s.releases.mu.Unlock()
	return m, nil
}

// agentAsset is the release file for the platform an agent reported.
func agentAsset(report store.JSON) string {
	var r struct {
		OS   string `json:"os"`
		Arch string `json:"arch"`
	}
	_ = json.Unmarshal(report, &r)
	asset := "phoedox-agent-" + r.OS + "-" + r.Arch
	if r.OS == "windows" {
		asset += ".exe"
	}
	if !releaseAssetRe.MatchString(asset) {
		return ""
	}
	return asset
}

// agentUpdateAction offers "agent.update" when a newer release exists.
func (s *Service) agentUpdateAction(ctx context.Context, ag store.Agent) (AgentAction, bool) {
	asset := agentAsset(ag.LastReport)
	if asset == "" || ag.Version == "" || ag.Version == "dev" {
		return AgentAction{}, false
	}
	tag, err := s.latestAgentRelease(ctx)
	if err != nil || strings.TrimPrefix(tag, "v") == strings.TrimPrefix(ag.Version, "v") {
		return AgentAction{}, false
	}
	sums, err := s.releaseSums(ctx, tag)
	if err != nil || sums[asset] == "" {
		return AgentAction{}, false
	}
	return AgentAction{Action: "agent.update", Args: []string{AgentReleasePath(tag, asset), sums[asset]}}, true
}

// AgentRelease returns the local file of a release asset for an enrolled
// agent, downloading and verifying it against SHA256SUMS on first use.
func (s *Service) AgentRelease(ctx context.Context, token, tag, asset string) (string, error) {
	if _, err := s.DB.AgentByToken(ctx, hashToken(strings.TrimSpace(token))); err != nil {
		return "", ErrForbidden
	}
	if !releaseTagRe.MatchString(tag) || !releaseAssetRe.MatchString(asset) || s.AgentReleaseDir == "" {
		return "", ErrNotFound
	}
	file := filepath.Join(s.AgentReleaseDir, tag, asset)
	if _, err := os.Stat(file); err == nil {
		return file, nil
	}
	sums, err := s.releaseSums(ctx, tag)
	if err != nil {
		return "", err
	}
	want := sums[asset]
	if want == "" {
		return "", ErrNotFound
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.AgentReleases+"/download/"+tag+"/"+asset, nil)
	if err != nil {
		return "", err
	}
	res, err := s.releaseClient().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", asset, res.Status)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".download-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(res.Body, 200<<20)); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return "", fmt.Errorf("download %s: checksum does not match SHA256SUMS", asset)
	}
	return file, os.Rename(tmp.Name(), file)
}
