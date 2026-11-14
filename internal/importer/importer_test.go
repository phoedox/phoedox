package importer

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, _ := w.Create(name)
		f.Write([]byte(body))
	}
	w.Close()
	return buf.Bytes()
}

func TestMarkdownZip(t *testing.T) {
	data := zipOf(t, map[string]string{
		"Vault/Start.md":            "---\ntitle: Willkommen\ntags: [a]\n---\nSiehe [[Netz/Router|den Router]] und [[Router]].\n",
		"Vault/Netz/Router.md":      "# Fritzbox\r\n\r\nSteht im [[Flur]]. ![[foto.png]]",
		"Vault/.obsidian/app.json":  "{}",
		"Vault/foto.png":            "png",
		"__MACOSX/Vault/._Start.md": "x",
	})
	pages, skipped, err := MarkdownZip(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || len(skipped) != 1 || skipped[0] != "Vault/foto.png" {
		t.Fatalf("%+v %v", pages, skipped)
	}
	if pages[0].Title != "Willkommen" || pages[0].Category != "" || strings.HasPrefix(pages[0].Markdown, "---") {
		t.Fatalf("front matter: %+v", pages[0])
	}
	if pages[1].Title != "Fritzbox" || pages[1].Category != "Netz" || pages[1].Key != "Router" || strings.Contains(pages[1].Markdown, "\r") {
		t.Fatalf("router: %+v", pages[1])
	}
	out := ResolveWikiLinks(pages[0].Markdown, func(k string) string {
		if k == "Router" || k == "Netz/Router" {
			return "/p/x/d/fritzbox"
		}
		return ""
	})
	if !strings.Contains(out, "[den Router](/p/x/d/fritzbox)") || !strings.Contains(out, "[Router](/p/x/d/fritzbox)") {
		t.Fatalf("links: %s", out)
	}
	if got := ResolveWikiLinks("im [[Flur]]. ![[foto.png]]", func(string) string { return "" }); got != "im Flur. foto.png" {
		t.Fatalf("unresolved: %q", got)
	}
	if _, _, err := MarkdownZip([]byte("nope")); err != ErrNotZip {
		t.Fatal(err)
	}
}

func TestOpenAPI(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: Shop, version: "1.2"}
servers: [{url: https://api.example.com/v1}]
components: {securitySchemes: {bearer: {type: http, scheme: bearer}}}
paths:
  /orders:
    get: {tags: [Orders], summary: List orders}
    post:
      tags: [Orders]
      summary: Create
      requestBody: {content: {application/json: {example: {item: "x", qty: 2}}}}
  /health:
    get: {summary: Health}
`
	api, err := OpenAPI([]byte(spec))
	if err != nil {
		t.Fatal(err)
	}
	if api.Title != "Shop" || api.Version != "1.2" || api.Server != "https://api.example.com/v1" || len(api.Pages) != 2 {
		t.Fatalf("%+v", api)
	}
	orders := api.Pages[1]
	for _, want := range []string{"# Orders", "## POST /orders", "POST {{base}}/orders", "Authorization: Bearer {{token}}", `"qty": 2`, "expect status < 400"} {
		if !strings.Contains(orders.Markdown, want) {
			t.Errorf("missing %q in\n%s", want, orders.Markdown)
		}
	}
	if _, err := OpenAPI([]byte(`{"hello": 1}`)); err != ErrNotOpenAPI {
		t.Fatal(err)
	}
	sw, err := OpenAPI([]byte(`{"swagger":"2.0","info":{"title":"Old"},"host":"x.org","basePath":"/api","schemes":["http"],"paths":{"/a":{"get":{}}}}`))
	if err != nil || sw.Server != "http://x.org/api" {
		t.Fatalf("swagger 2: %v %+v", err, sw)
	}
}
