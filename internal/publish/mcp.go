package publish

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

// Public read-only MCP endpoint of a publication (<base>/mcp, ADR-0045):
// AI clients can list, search and read the published pages without an
// account. Only public publications have it; it never sees anything that is
// not on the site.

type pubListIn struct{}

type pubSearchIn struct {
	Query string `json:"query" jsonschema:"words to find in titles and text"`
}

type pubPageIn struct {
	Project string `json:"project" jsonschema:"project slug as returned by list_pages"`
	Page    string `json:"page" jsonschema:"page slug as returned by list_pages or search"`
	Lang    string `json:"lang,omitempty" jsonschema:"language code of a translation, e.g. de"`
}

type pubPage struct {
	Project string `json:"project"`
	Page    string `json:"page"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

func textResult(v any) *sdk.CallToolResult {
	var text string
	if s, ok := v.(string); ok {
		text = s
	} else {
		b, _ := json.MarshalIndent(v, "", "  ")
		text = string(b)
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}
}

func (s *Site) mcpServer(r *http.Request, p store.Publication, base string, site []core.PubProject) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "phoedox-publication", Version: "1"},
		&sdk.ServerOptions{Instructions: "Read-only access to the published documentation \"" + p.Title + "\". Use list_pages or search, then get_page for the Markdown of a page."})
	url := func(project, page string) string { return s.absolute(r, base+"/"+project+"/"+page) }
	sdk.AddTool(srv, &sdk.Tool{Name: "list_pages", Description: "All published pages with project, slug, title, type and URL."},
		func(ctx context.Context, _ *sdk.CallToolRequest, _ pubListIn) (*sdk.CallToolResult, any, error) {
			out := []pubPage{}
			for _, pp := range site {
				for _, d := range pp.Docs {
					out = append(out, pubPage{Project: pp.Project.Slug, Page: d.Slug, Title: d.Title, Type: d.Type, URL: url(pp.Project.Slug, d.Slug)})
				}
			}
			return textResult(out), nil, nil
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "search", Description: "Find published pages whose title or text contain all words of the query."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in pubSearchIn) (*sdk.CallToolResult, any, error) {
			out := []pubPage{}
			for i, h := range core.PubSearch(site, strings.TrimSpace(in.Query)) {
				if i == 25 {
					break
				}
				out = append(out, pubPage{Project: h.Project.Project.Slug, Page: h.Doc.Slug, Title: h.Doc.Title, Type: h.Doc.Type, URL: url(h.Project.Project.Slug, h.Doc.Slug), Snippet: h.Snippet})
			}
			return textResult(out), nil, nil
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "get_page", Description: "The Markdown of one published page (optionally a translation)."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in pubPageIn) (*sdk.CallToolResult, any, error) {
			_, d, err := s.Core.PubDocument(ctx, site, in.Project, in.Page, in.Lang)
			if err != nil {
				return nil, nil, errNotPublished
			}
			return textResult(docMarkdown(d) + "\n\nSource: " + url(in.Project, d.Slug) + "\n"), nil, nil
		})
	return srv
}

type pubError string

func (e pubError) Error() string { return string(e) }

const errNotPublished = pubError("this page is not part of the publication")

// mcp answers MCP requests (stateless, JSON responses).
func (s *Site) mcp(w http.ResponseWriter, r *http.Request, p store.Publication, base string, site []core.PubProject) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	srv := s.mcpServer(r, p, base, site)
	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	h.ServeHTTP(w, r)
}
