package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/store"
)

func TestDocumentationGaps(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Lab"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	for _, in := range []EntityInput{{Kind: "router", Name: "Router"}, {Kind: "server", Name: "NAS"}, {Kind: "room", Name: "Hall"}, {Kind: "service", Name: "Backup"}} {
		if _, err := s.UpsertEntity(ctx, owner, p.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Link(ctx, owner, p.ID, EdgeInput{From: "nas", Rel: "located_in", To: "hall"}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Network", Markdown: "The @router is in the hall, next to the @ghost switch. It has been there since 2019."})
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Todo", Markdown: "tbd"})

	r, err := s.DocumentationGaps(ctx, reader, p.Slug, 0)
	if err != nil {
		t.Fatal(err)
	}
	has := func(kind, subject string) bool {
		for _, g := range r.Gaps {
			if g.Kind == kind && g.Subject == subject {
				return true
			}
		}
		return false
	}
	if !has("broken_mention", "network") || !has("empty_page", "todo") {
		t.Fatalf("page gaps: %+v", r.Gaps)
	}
	if !has("unplaced_device", "router") || has("unplaced_device", "nas") || has("unplaced_device", "hall") {
		t.Fatalf("placement: %+v", r.Gaps)
	}
	// Entries in the graph or mentioned on a page count as documented.
	if !has("undocumented_entry", "backup") || has("undocumented_entry", "nas") || has("undocumented_entry", "router") {
		t.Fatalf("mentions: %+v", r.Gaps)
	}
	if r.Counts["broken_mention"] != 1 {
		t.Fatalf("counts: %v", r.Counts)
	}
	if _, err := s.DocumentationGaps(ctx, Actor{}, p.Slug, 0); err == nil {
		t.Fatal("anonymous read gaps")
	}
}

func TestChangeSummary(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Lab"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleEditor)
	since := time.Now().Add(-time.Minute)
	page, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Network", Markdown: "first"})
	if _, err := s.UpdateDocument(ctx, reader, p.Slug, page.Document.Slug, DocumentPatch{Markdown: strp("second")}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "router", Name: "Router"})

	sum, err := s.Changes(ctx, reader, p.Slug, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Pages) != 1 || !sum.Pages[0].Created || sum.Pages[0].Edits != 1 || len(sum.Pages[0].Authors) != 2 {
		t.Fatalf("pages: %+v", sum.Pages)
	}
	if len(sum.Entries) != 1 || !sum.Entries[0].Added || sum.Entries[0].Slug != "router" {
		t.Fatalf("entries: %+v", sum.Entries)
	}
	if later, _ := s.Changes(ctx, reader, p.Slug, time.Now().Add(time.Minute)); !later.Empty() {
		t.Fatalf("future: %+v", later)
	}

	// Weekly summary: only on Monday morning, once per week.
	monday := time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC)
	s.SendSummaries(ctx, monday.Add(-24*time.Hour))
	if list, _ := s.Notifications(ctx, reader, false, 0); len(list.Items) != 0 {
		t.Fatalf("sunday: %+v", list.Items)
	}
	s.SendSummaries(ctx, monday)
	s.SendSummaries(ctx, monday.Add(time.Hour))
	list, _ := s.Notifications(ctx, reader, false, 0)
	n := 0
	for _, it := range list.Items {
		if it.Kind == "project.summary" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("summaries: %+v", list.Items)
	}
}

func TestArchiveLink(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	_ = os.MkdirAll(s.TempDir, 0o700)
	s.RequestPolicy = requests.Policy{AllowPrivate: true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4 manual"))
	}))
	defer srv.Close()
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Lab"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	doc, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "link", Title: "Manual", Link: &Link{URL: srv.URL + "/manual.pdf"}})
	if err != nil {
		t.Fatal(err)
	}
	gone, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "link", Title: "Gone", Link: &Link{URL: srv.URL + "/gone"}})
	page, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Page", Markdown: "x"})

	if _, err := s.ArchiveLink(ctx, reader, p.Slug, doc.Document.Slug); err == nil {
		t.Fatal("reader archived")
	}
	if _, err := s.ArchiveLink(ctx, owner, p.Slug, page.Document.Slug); err == nil {
		t.Fatal("archived a markdown page")
	}
	res, err := s.ArchiveLink(ctx, owner, p.Slug, doc.Document.Slug)
	if err != nil {
		t.Fatal(err)
	}
	var l Link
	_ = json.Unmarshal(res.Document.Content, &l)
	if l.Archive == nil || l.Archive.Mime != "application/pdf" {
		t.Fatalf("archive: %+v", l)
	}
	if _, err := s.ArchiveLink(ctx, owner, p.Slug, gone.Document.Slug); err == nil {
		t.Fatal("archived a 404")
	}
	// Editing the description keeps the copy.
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, doc.Document.Slug, DocumentPatch{Link: &Link{URL: l.URL, Description: "PDF"}}); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Document(ctx, owner, p.Slug, doc.Document.Slug)
	var l2 Link
	_ = json.Unmarshal(again.Content, &l2)
	if l2.Archive == nil || l2.Description != "PDF" {
		t.Fatalf("archive lost on edit: %+v", l2)
	}
	all, err := s.ArchiveAllLinks(ctx, owner, p.Slug)
	if failed, _ := all["failed"].([]string); err != nil || len(failed) != 1 || failed[0] != "gone" {
		t.Fatalf("all: %v %v", all, err)
	}
}
