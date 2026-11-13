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
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return requests.Result{}, err
	}
	doc, err := s.findDoc(ctx, p.ID, docRef)
	if err != nil {
		return requests.Result{}, err
	}
	var c struct {
		Markdown string `json:"markdown"`
	}
	_ = json.Unmarshal(doc.Content, &c)
	blocks := requests.Blocks(c.Markdown)
	if doc.Type != store.DocMarkdown || index < 0 || index >= len(blocks) {
		return requests.Result{}, invalid("validation.request_missing", nil, "the document has no request block %d", index)
	}
	req, err := requests.Parse(blocks[index])
	if err != nil {
		return requests.Result{}, invalid("validation.request_syntax", nil, "the request block cannot be read")
	}
	if !requests.Safe(req.Method) {
		if a.Via == "mcp" {
			return requests.Result{}, ErrForbidden
		}
		if rank[p.Role] < rank[store.RoleEditor] {
			return requests.Result{}, ErrForbidden
		}
	}
	values, secrets, err := s.envValues(ctx, p.ID)
	if err != nil {
		return requests.Result{}, err
	}
	filled, missing := req.Fill(values)
	if len(missing) > 0 {
		return requests.Result{}, invalid("validation.request_variable", map[string]any{"name": missing[0]}, "the variable %s is not set in the project environment", missing[0])
	}
	client := s.RequestPolicy.Client()
	res := requests.Run(ctx, client, filled)
	res.Mask(secrets)
	host := ""
	if u, err := url.Parse(filled.URL); err == nil {
		host = u.Host
	}
	s.event(ctx, a, "request.run", doc.ID, map[string]any{"index": index, "method": req.Method, "host": maskHost(host, secrets), "status": res.Status})
	return res, nil
}

func maskHost(host string, secrets []string) string {
	r := requests.Result{URL: host}
	r.Mask(secrets)
	return r.URL
}
