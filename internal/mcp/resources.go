package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

// MCP resources: pages and entries can be attached by clients that support
// resources. URIs: phoedox://<project>/doc/<slug> and
// phoedox://<project>/entity/<slug>; phoedox://projects lists the projects.
func registerResources(s *sdk.Server, svc *core.Service) {
	s.AddResource(&sdk.Resource{URI: "phoedox://projects", Name: "projects", Title: "Phoedox projects", MIMEType: "application/json",
		Description: "Projects visible to you with their categories."},
		func(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			a, err := actorOfExtra(req.Extra)
			if err != nil {
				return nil, err
			}
			ps, err := svc.Projects(ctx, a)
			if err != nil {
				return nil, friendly(err)
			}
			type row struct {
				Slug, Name, Description string
			}
			out := make([]row, len(ps))
			for i, p := range ps {
				out[i] = row{p.Slug, p.Name, p.Description}
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: req.Params.URI, MIMEType: "application/json", Text: string(b)}}}, nil
		})
	s.AddResourceTemplate(&sdk.ResourceTemplate{URITemplate: "phoedox://{project}/doc/{slug}", Name: "document", Title: "Phoedox page",
		MIMEType: "text/markdown", Description: "A page or decision as Markdown."}, func(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return readResource(ctx, svc, req, "doc")
	})
	s.AddResourceTemplate(&sdk.ResourceTemplate{URITemplate: "phoedox://{project}/entity/{slug}", Name: "entity", Title: "Phoedox entry",
		MIMEType: "text/markdown", Description: "The context pack of an entry of the knowledge graph."}, func(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return readResource(ctx, svc, req, "entity")
	})
}

func readResource(ctx context.Context, svc *core.Service, req *sdk.ReadResourceRequest, kind string) (*sdk.ReadResourceResult, error) {
	a, err := actorOfExtra(req.Extra)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(req.Params.URI)
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if err != nil || u.Scheme != "phoedox" || len(parts) != 2 || parts[0] != kind {
		return nil, sdk.ResourceNotFoundError(req.Params.URI)
	}
	project, slug := u.Host, parts[1]
	var text string
	if kind == "entity" {
		text, err = svc.EntityContext(ctx, a, project, slug)
	} else {
		var d store.Document
		if d, err = svc.Document(ctx, a, project, slug); err == nil {
			text = docMarkdown(d)
		}
	}
	if err != nil {
		return nil, sdk.ResourceNotFoundError(req.Params.URI)
	}
	return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: req.Params.URI, MIMEType: "text/markdown", Text: text}}}, nil
}

// docMarkdown renders a document as Markdown for a resource.
func docMarkdown(d store.Document) string {
	c := map[string]any{}
	_ = json.Unmarshal(d.Content, &c)
	str := func(k string) string { v, _ := c[k].(string); return v }
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", d.Title)
	switch d.Type {
	case "adr":
		fmt.Fprintf(&b, "Status: %s · %s\n\n", str("status"), str("date"))
		for _, k := range []string{"context", "decision", "consequences", "alternatives"} {
			if v := str(k); v != "" {
				fmt.Fprintf(&b, "## %s\n\n%s\n\n", strings.ToUpper(k[:1])+k[1:], v)
			}
		}
	case "markdown":
		b.WriteString(str("markdown"))
	default:
		b.WriteString(str("description"))
		if u := str("url"); u != "" {
			b.WriteString("\n\n" + u)
		}
	}
	return b.String()
}
