// Package storeclient talks to the plugin store (phoedox-store). Packages
// it downloads are checked by the caller (hash and store signature) before
// anything is installed.
package storeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/plugins"
)

// Client is a store client.
type Client struct {
	Base string // e.g. https://store.phoedox.com
	HTTP *http.Client
}

// New returns a client for a store URL.
func New(base string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// SignatureHeader carries "<keyId>:<base64 signature>" of a download.
const SignatureHeader = "Phoedox-Signature"

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return fmt.Errorf("%s (%d)", e.Error, res.StatusCode)
		}
		return fmt.Errorf("store answered %d", res.StatusCode)
	}
	return json.Unmarshal(data, out)
}

// Catalog lists the newest compatible version of every plugin.
func (c *Client) Catalog(ctx context.Context, version, hostAPI, channel string) ([]core.CatalogEntry, error) {
	var out struct {
		Plugins []core.CatalogEntry `json:"plugins"`
	}
	q := url.Values{"phoedox": {version}, "hostApi": {hostAPI}}
	if channel == "beta" {
		q.Set("channel", "beta")
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/catalog?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	if out.Plugins == nil {
		out.Plugins = []core.CatalogEntry{}
	}
	return out.Plugins, nil
}

// Collections lists the store's curated plugin collections.
func (c *Client) Collections(ctx context.Context) ([]core.PluginCollection, error) {
	var out struct {
		Collections []core.PluginCollection `json:"collections"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/collections", nil, &out); err != nil {
		return nil, err
	}
	if out.Collections == nil {
		out.Collections = []core.PluginCollection{}
	}
	return out.Collections, nil
}

// Download fetches a package and the store's signature of it.
func (c *Client) Download(ctx context.Context, id, version string) ([]byte, plugins.Signature, error) {
	var sig plugins.Signature
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/api/v1/plugins/"+url.PathEscape(id)+"/versions/"+url.PathEscape(version)+"/download", nil)
	if err != nil {
		return nil, sig, err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, sig, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, sig, fmt.Errorf("store answered %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, plugins.MaxPackage+1))
	if err != nil {
		return nil, sig, err
	}
	if len(data) > plugins.MaxPackage {
		return nil, sig, fmt.Errorf("package too large")
	}
	sig.KeyID, sig.Sig, _ = strings.Cut(res.Header.Get(SignatureHeader), ":")
	return data, sig, nil
}

// Redeem trades a license code for a signed license file.
func (c *Client) Redeem(ctx context.Context, code, pluginID, orgID, instanceID string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/v1/licenses/redeem", map[string]string{"code": code, "pluginId": pluginID, "orgId": orgID, "instanceId": instanceID}, &out)
	return out, err
}

// Recalls asks which of the installed versions were recalled.
func (c *Client) Recalls(ctx context.Context, installed map[string]string) (map[string]core.Recall, error) {
	var out struct {
		Recalls map[string]core.Recall `json:"recalls"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/recalls", map[string]any{"installed": installed}, &out); err != nil {
		return nil, err
	}
	return out.Recalls, nil
}
