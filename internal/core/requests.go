package core

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/store"
)

// RunRequest executes request block number index (0-based) of a stored
// Markdown document. Only blocks saved in documents run, so readers cannot
// build their own requests around the project's secrets. Reading methods
// need read access; others need edit access and are refused for AI clients.
func (s *Service) RunRequest(ctx context.Context, a Actor, ref, docRef string, index int) (requests.Result, error) {
	// Request blocks use the project's secrets: project readers only, and
	// only on documents they can see.
	if _, err := s.project(ctx, a, ref, store.RoleReader); err != nil {
		return requests.Result{}, err
	}
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return requests.Result{}, err
	}
	req, err := blockRequest(doc, index)
	if err != nil {
		return requests.Result{}, err
	}
	if !requests.Safe(req.Method) {
		if a.Via == "mcp" {
			return requests.Result{}, ErrForbidden
		}
		if rank[p.Role] < rank[store.RoleEditor] {
			return requests.Result{}, ErrForbidden
		}
	}
	res, host, err := s.execRequest(ctx, p.ID, req)
	if err != nil {
		return res, err
	}
	s.event(ctx, a, "request.run", doc.ID, map[string]any{"index": index, "method": req.Method, "host": host, "status": res.Status})
	return res, nil
}

// blockRequest parses request block number index of a page.
func blockRequest(doc store.Document, index int) (requests.Request, error) {
	var c struct {
		Markdown string `json:"markdown"`
	}
	_ = json.Unmarshal(doc.Content, &c)
	blocks := requests.Blocks(c.Markdown)
	if doc.Type != store.DocMarkdown || index < 0 || index >= len(blocks) {
		return requests.Request{}, invalid("validation.request_missing", nil, "the document has no request block %d", index)
	}
	req, err := requests.Parse(blocks[index])
	if err != nil {
		return requests.Request{}, invalid("validation.request_syntax", nil, "the request block cannot be read")
	}
	return req, nil
}

// execRequest fills a request from the project environment and sends it;
// secrets are masked in the result and in the returned host.
func (s *Service) execRequest(ctx context.Context, projectID string, req requests.Request) (requests.Result, string, error) {
	values, secrets, err := s.envValues(ctx, projectID)
	if err != nil {
		return requests.Result{}, "", err
	}
	filled, missing := req.Fill(values)
	if len(missing) > 0 {
		return requests.Result{}, "", invalid("validation.request_variable", map[string]any{"name": missing[0]}, "the variable %s is not set in the project environment", missing[0])
	}
	res := requests.Run(ctx, s.RequestPolicy.Client(), filled)
	res.Mask(secrets)
	host := ""
	if u, err := url.Parse(filled.URL); err == nil {
		host = u.Host
	}
	return res, maskHost(host, secrets), nil
}

func maskHost(host string, secrets []string) string {
	r := requests.Result{URL: host}
	r.Mask(secrets)
	return r.URL
}
