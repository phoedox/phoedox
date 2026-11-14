package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestImports(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Wiki"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", "reader")
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Start"})

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range map[string]string{"notes/Start.md": "# Start\n\nWeiter zu [[Netz]].", "notes/Technik/Netz.md": "Zurück zum [[Start]]."} {
		f, _ := w.Create(name)
		f.Write([]byte(body))
	}
	w.Close()
	if _, err := s.ImportMarkdown(ctx, reader, p.Slug, buf.Bytes()); err == nil {
		t.Fatal("readers cannot import")
	}
	if _, err := s.ImportMarkdown(ctx, owner, p.Slug, []byte("x")); invalidKey(err) != "validation.import_zip" {
		t.Fatalf("not a zip: %v", err)
	}
	res, err := s.ImportMarkdown(ctx, owner, p.Slug, buf.Bytes())
	if err != nil || len(res.Created) != 2 {
		t.Fatalf("%v %+v", err, res)
	}
	// "Start" existed already, so the imported one is start-2; links follow the new slugs.
	netz, err := s.Document(ctx, owner, p.Slug, "netz")
	if err != nil || netz.Category != "Technik" {
		t.Fatalf("netz: %v %+v", err, netz)
	}
	var c struct{ Markdown string }
	_ = json.Unmarshal(netz.Content, &c)
	if !strings.Contains(c.Markdown, "[Start](/p/"+p.Slug+"/d/start-2)") {
		t.Fatalf("wiki link: %s", c.Markdown)
	}

	spec := `{"openapi":"3.0.0","info":{"title":"Shop","version":"1"},"servers":[{"url":"https://api.example.com"}],
		"paths":{"/orders":{"get":{"tags":["Orders"],"summary":"List"}}}}`
	res, err = s.ImportOpenAPI(ctx, owner, p.Slug, []byte(spec))
	if err != nil || len(res.Created) != 1 || res.Entity != "shop" {
		t.Fatalf("openapi: %v %+v", err, res)
	}
	page, _ := s.Document(ctx, owner, p.Slug, res.Created[0])
	_ = json.Unmarshal(page.Content, &c)
	if page.Category != "API/Shop" || !strings.Contains(c.Markdown, "GET {{base}}/orders") || !strings.Contains(c.Markdown, "@shop") {
		t.Fatalf("api page: %+v %s", page, c.Markdown)
	}
	if e, _ := s.Entity(ctx, owner, p.Slug, "shop"); len(e.Documents) != 1 {
		t.Fatalf("service linked to its page: %+v", e.Documents)
	}
}
