package publish

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

func TestWriteOffline(t *testing.T) {
	content := func(v any) store.JSON { b, _ := json.Marshal(v); return b }
	d := core.OfflineData{
		Project: store.Project{Slug: "haus", Name: "Haus <Nord>"},
		Docs: []store.Document{
			{ID: "d1", Slug: "netz", Title: "Netz", Type: store.DocMarkdown, Category: "Technik",
				Content: content(map[string]string{"markdown": "Siehe [[Notfall]] und ![Plan](/api/v1/files/F1/plan.png)\n\n<script>alert(1)</script>"})},
			{ID: "d2", Slug: "notfall", Title: "Notfall", Type: store.DocMarkdown, Content: content(map[string]string{"markdown": "Hauptschalter im Keller"})},
			{ID: "d3", Slug: "adr-1", Title: "NAS", Type: store.DocADR, Number: 1, Content: content(map[string]string{"status": "accepted", "decision": "Synology"})},
		},
		Entities: []store.Entity{{ID: "e1", Slug: "router", Name: "Router", Kind: "router", Attrs: content(map[string]string{"ip": "192.168.1.1"})},
			{ID: "e2", Slug: "keller", Name: "Keller", Kind: "room", Attrs: content(map[string]string{})}},
		Edges: []store.Edge{{From: "e1", To: "e2", Rel: "located_in", FromSlug: "router", ToSlug: "keller", FromName: "Router", ToName: "Keller"}},
		Files: []store.File{{ID: "F1", Name: "plan.png", EntityID: "e1"}},
	}
	open := func(context.Context, store.File) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("PNG")), nil }
	var buf bytes.Buffer
	if err := WriteOffline(context.Background(), &buf, d, open, "de", time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		files[f.Name] = string(b)
	}
	for _, want := range []string{"index.html", "p-netz.html", "p-notfall.html", "p-adr-1.html", "entries.html", "e-router.html", "e-keller.html", "files/F1/plan.png", "README.txt"} {
		if _, ok := files[want]; !ok {
			t.Fatalf("missing %s in %v", want, keysOf(files))
		}
	}
	netz := files["p-netz.html"]
	if !strings.Contains(netz, `href="p-notfall.html"`) || !strings.Contains(netz, `src="files/F1/plan.png"`) || strings.Contains(netz, "<script>alert") {
		t.Fatalf("page: %s", netz)
	}
	if !strings.Contains(files["index.html"], "Haus &lt;Nord&gt;") || !strings.Contains(files["index.html"], "ADR-0001") || !strings.Contains(files["index.html"], "Notfallkopie vom 2026-11-15") {
		t.Fatalf("index: %s", files["index.html"])
	}
	r := files["e-router.html"]
	if !strings.Contains(r, "192.168.1.1") || !strings.Contains(r, `href="e-keller.html"`) || !strings.Contains(r, `href="files/F1/plan.png"`) {
		t.Fatalf("entry: %s", r)
	}
	if files["files/F1/plan.png"] != "PNG" {
		t.Fatal("attachment content")
	}
}

func keysOf(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
