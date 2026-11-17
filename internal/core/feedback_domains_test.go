package core

import (
	"context"
	"testing"
)

func TestFeedbackPortalDomain(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	s.PublicURL = "https://app.example"
	txt := map[string][]string{}
	s.LookupTXT = func(_ context.Context, name string) ([]string, error) { return txt[name], nil }
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Game"})
	settings := DefaultPortalSettings()
	settings.Anonymous = true
	v, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, "", PortalInput{Handle: "game", Name: "Game", Settings: &settings})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetFeedbackPortalDomain(ctx, reader, p.Slug, v.ID, "feedback.example.org"); err == nil {
		t.Fatal("reader set a domain")
	}
	for _, bad := range []string{"no_dots", "app.example"} {
		if _, err := s.SetFeedbackPortalDomain(ctx, owner, p.Slug, v.ID, bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	d, err := s.SetFeedbackPortalDomain(ctx, owner, p.Slug, v.ID, "Feedback.Example.org.")
	if err != nil || d.Domain != "feedback.example.org" || d.Verified || d.RecordName != "_phoedox.feedback.example.org" || d.Target != "app.example" {
		t.Fatalf("set: %v %+v", err, d)
	}
	if s.FeedbackPortalForHost(ctx, "feedback.example.org") != "" {
		t.Fatal("unverified domain serves the portal")
	}
	if _, err := s.VerifyFeedbackPortalDomain(ctx, owner, p.Slug, v.ID); invalidKey(err) != "validation.domain_unverified" {
		t.Fatalf("no record: %v", err)
	}
	txt["_phoedox.feedback.example.org"] = []string{"other", d.RecordText}
	if d, err = s.VerifyFeedbackPortalDomain(ctx, owner, p.Slug, v.ID); err != nil || !d.Verified {
		t.Fatalf("verify: %v %+v", err, d)
	}
	if h := s.FeedbackPortalForHost(ctx, "feedback.example.org:443"); h != "game" {
		t.Fatalf("host: %q", h)
	}
	// Another portal cannot take the domain.
	v2, _ := s.SaveFeedbackPortal(ctx, owner, p.Slug, "", PortalInput{Handle: "game-two", Name: "Two", Settings: &settings})
	if _, err := s.SetFeedbackPortalDomain(ctx, owner, p.Slug, v2.ID, "feedback.example.org"); invalidKey(err) != "validation.domain_taken" {
		t.Fatalf("taken: %v", err)
	}
	views, _ := s.FeedbackPortals(ctx, owner, p.Slug)
	for _, pv := range views {
		if pv.ID == v.ID && (pv.DomainInfo == nil || !pv.DomainInfo.Verified) {
			t.Fatalf("view: %+v", pv.DomainInfo)
		}
	}
	// Removing it ends serving.
	if _, err := s.SetFeedbackPortalDomain(ctx, owner, p.Slug, v.ID, ""); err != nil || s.FeedbackPortalForHost(ctx, "feedback.example.org") != "" {
		t.Fatalf("remove: %v", err)
	}
}
