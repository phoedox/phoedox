package core

import (
	"context"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestAutoLinks(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Spiel", Features: &[]string{"decisions"}})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocADR, Title: "Use Postgres", ExtRef: "D-640", ADR: &ADR{Status: "accepted", Decision: "Postgres"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetAutoLinkRules(ctx, reader, p.Slug, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("only owners set rules: %v", err)
	}
	for key, rules := range map[string][]AutoLinkRule{
		"validation.autolink_pattern": {{Pattern: "(", To: "url", URL: "https://x/$0"}},
		"validation.autolink_url":     {{Pattern: `JIRA-\d+`, To: "url", URL: "javascript:alert(1)"}},
		"validation.autolink_to":      {{Pattern: `JIRA-\d+`, To: "mail"}},
	} {
		if _, err := s.SetAutoLinkRules(ctx, owner, p.Slug, rules); invalidKey(err) != key {
			t.Errorf("%s: %v", key, err)
		}
	}
	if _, err := s.SetAutoLinkRules(ctx, owner, p.Slug, []AutoLinkRule{{Pattern: "a*", To: "ref"}}); invalidKey(err) != "validation.autolink_pattern" {
		t.Errorf("patterns that match nothing are refused: %v", err)
	}
	if _, err := s.SetAutoLinkRules(ctx, owner, p.Slug, []AutoLinkRule{{Pattern: `JIRA-(\d+)`, To: "url", URL: "https://jira.example.org/browse/JIRA-$1", Note: "Jira"}}); err != nil {
		t.Fatal(err)
	}
	al, err := s.ProjectAutoLinks(ctx, reader, p.Slug)
	if err != nil || len(al.Rules) != 1 || al.Refs["ADR-0001"].Title != "Use Postgres" || al.Refs["D-640"].Href != "/p/"+p.Slug+"/d/use-postgres" {
		t.Fatalf("autolinks: %v %+v", err, al)
	}
}
