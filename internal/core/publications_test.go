package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

// TestPublications covers ADR-0006: only owners publish, only published,
// unrestricted pages are visible, drafts never.
func TestPublications(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	s.HashPassword = func(pw string) (string, error) { return "h:" + pw, nil }
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Produkt"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Start", Markdown: "Hallo Welt"})
	secret, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Intern", Markdown: "geheim"})
	if err := s.DB.SetRestricted(ctx, secret.Document.ID, true); err != nil {
		t.Fatal(err)
	}

	if _, err := s.CreatePublication(ctx, reader, PublicationInput{Title: "Doku", Projects: []string{p.Slug}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers cannot publish: %v", err)
	}
	if _, err := s.CreatePublication(ctx, owner, PublicationInput{Title: "Doku", Projects: []string{p.Slug}, Domain: "http://x"}); invalidKey(err) != "validation.pub_domain" {
		t.Fatalf("domain: %v", err)
	}
	if _, err := s.CreatePublication(ctx, owner, PublicationInput{Title: "Doku", Projects: []string{p.Slug}, Access: "password"}); invalidKey(err) != "validation.pub_password" {
		t.Fatalf("password needed: %v", err)
	}
	if _, err := s.CreatePublication(ctx, owner, PublicationInput{Title: "Doku", Projects: []string{p.Slug}, CSS: "</style><script>"}); invalidKey(err) != "validation.pub_css" {
		t.Fatalf("css: %v", err)
	}
	pub, err := s.CreatePublication(ctx, owner, PublicationInput{Title: "Doku", Projects: []string{p.Slug}, Domain: "Docs.Example.org"})
	if err != nil || pub.Domain != "docs.example.org" || pub.Slug != "doku" {
		t.Fatalf("create: %v %+v", err, pub)
	}
	// Personal projects: one own domain per project.
	if _, err := s.CreatePublication(ctx, owner, PublicationInput{Title: "Zwei", Projects: []string{p.Slug}, Domain: "other.example.org"}); invalidKey(err) != "validation.domain_limit" {
		t.Fatalf("second domain: %v", err)
	}
	if _, err := s.CreatePublication(ctx, owner, PublicationInput{Title: "Drei", Projects: []string{p.Slug}}); err != nil {
		t.Fatalf("without a domain: %v", err)
	}
	if got, err := s.DB.PublicationByDomain(ctx, "docs.example.org"); err != nil || got.ID != pub.ID {
		t.Fatalf("by domain: %v", err)
	}

	site, err := s.Site(ctx, pub.Publication)
	if err != nil || len(site) != 1 || len(site[0].Docs) != 1 || site[0].Docs[0].Title != "Start" {
		t.Fatalf("site must hide restricted pages: %v %+v", err, site)
	}
	// Drafts stay private until published.
	_, _ = s.StartDraft(ctx, owner, p.Slug, "start")
	_, _ = s.SaveDraft(ctx, owner, p.Slug, "start", DraftPatch{Markdown: strp("Entwurf")})
	if _, d, _ := s.PubDocument(ctx, site, p.Slug, "start", ""); markdownOf(d) != "Hallo Welt" {
		t.Fatalf("draft leaked: %q", markdownOf(d))
	}
	if hits := PubSearch(site, "welt"); len(hits) != 1 {
		t.Fatalf("search: %d", len(hits))
	}
	if hits := PubSearch(site, "geheim"); len(hits) != 0 {
		t.Fatal("restricted pages must not be found")
	}
	if list, _ := s.Publications(ctx, reader); len(list) != 0 {
		t.Fatal("readers do not see publications")
	}
	if err := s.DeletePublication(ctx, owner, pub.ID); err != nil {
		t.Fatal(err)
	}
}

// Publishing chosen pages of a code project (DEPLOY, CONFIG) with fixed
// links while the rest stays private.
func TestPublicationPages(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.PublicURL = "https://docs.example"
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Service"})
	deploy, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Deploy", Markdown: "steps"})
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Internals", Markdown: "private"})
	if _, err := s.CreatePublication(ctx, owner, PublicationInput{Title: "Ops", Projects: []string{p.Slug}, Pages: []string{p.Slug + "/nope"}}); invalidKey(err) != "validation.pub_page" {
		t.Fatalf("unknown page: %v", err)
	}
	pub, err := s.CreatePublication(ctx, owner, PublicationInput{Title: "Ops", Projects: []string{p.Slug}, Pages: []string{p.Slug + "/deploy"}})
	if err != nil || len(pub.PageLinks) != 1 || pub.PageLinks[0].URL != "https://docs.example/pub/ops/"+p.Slug+"/"+strings.ToLower(deploy.Document.ID) {
		t.Fatalf("create: %v %+v", err, pub.PageLinks)
	}
	site, _ := s.Site(ctx, pub.Publication)
	if len(site[0].Docs) != 1 || site[0].Docs[0].Slug != "deploy" {
		t.Fatalf("only chosen pages: %+v", site[0].Docs)
	}
	// The fixed link survives a rename.
	slug := "deployment"
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, "deploy", DocumentPatch{Slug: &slug}); err != nil {
		t.Fatal(err)
	}
	site, _ = s.Site(ctx, pub.Publication)
	if _, d, err := s.PubDocument(ctx, site, p.Slug, strings.ToLower(deploy.Document.ID), ""); err != nil || d.Slug != "deployment" {
		t.Fatalf("fixed link: %v %+v", err, d.Slug)
	}
}
