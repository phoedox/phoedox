package core

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestDPA(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	admin := Actor{User: store.User{ID: "sys", Name: "System", Email: "sys@x.de", IsAdmin: true}, Via: "web"}
	org, err := s.CreateOrg(ctx, admin, OrgInput{Name: "Kunde GmbH", AdminEmail: "owner@x.de"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrgMember(ctx, owner, org.Slug, "reader@x.de", store.OrgMemberRole); err != nil {
		t.Fatal(err)
	}
	// The placeholder applies until a text is published.
	info, err := s.OrgDPA(ctx, owner, org.Slug, "de")
	if err != nil || !info.Current.Placeholder || info.Current.Lang != "de" || !strings.Contains(info.Current.Body, "Platzhalter") || !info.CanAccept || info.Accepted != nil {
		t.Fatalf("placeholder: %v %+v", err, info)
	}
	if i, _ := s.OrgDPA(ctx, reader, org.Slug, "en"); i.CanAccept {
		t.Fatal("members only read")
	}
	if _, err := s.OrgDPA(ctx, stranger, org.Slug, "en"); err == nil {
		t.Fatal("outsiders")
	}
	in := DPAAcceptInput{VersionID: info.Current.ID, Lang: "de", SignerName: "Olga Owner", SignerRole: "Geschäftsführerin", Confirm: true}
	if _, err := s.AcceptDPA(ctx, reader, org.Slug, in); err != ErrForbidden {
		t.Fatalf("member: %v", err)
	}
	if _, err := s.AcceptDPA(ctx, admin, org.Slug, in); err == nil {
		t.Fatal("system admins do not sign for customers")
	}
	noConfirm := in
	noConfirm.Confirm = false
	if _, err := s.AcceptDPA(ctx, owner, org.Slug, noConfirm); invalidKey(err) != "validation.dpa_confirm" {
		t.Fatalf("confirm: %v", err)
	}
	acc, err := s.AcceptDPA(ctx, owner, org.Slug, in)
	if err != nil || acc.Company != "Kunde GmbH" || acc.Version != "0" || len(acc.TextSHA256) != 64 {
		t.Fatalf("accept: %v %+v", err, acc)
	}
	pdf, name, err := s.DPAPDF(ctx, reader, org.Slug, acc.ID)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) || !strings.HasSuffix(name, ".pdf") || !bytes.Contains(pdf, []byte("Olga Owner")) || !bytes.Contains(pdf, []byte(acc.TextSHA256)) {
		t.Fatalf("pdf: %v %s", err, name)
	}
	if info, _ = s.OrgDPA(ctx, owner, org.Slug, "de"); info.Accepted == nil || info.Accepted.ID != acc.ID {
		t.Fatalf("accepted: %+v", info)
	}

	// A published version needs a new acceptance; the old one stays in the history.
	if _, err := s.PublishDPA(ctx, owner, DPAVersionInput{Version: "x", BodyEN: "y"}); err == nil {
		t.Fatal("only system admins publish")
	}
	v, err := s.PublishDPA(ctx, admin, DPAVersionInput{Version: "2026-11", BodyEN: "# DPA\n\nFinal text.", BodyDE: "# AVV\n\nEndgültiger Text."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishDPA(ctx, admin, DPAVersionInput{Version: "2026-11", BodyEN: "again"}); invalidKey(err) != "validation.dpa_version_taken" {
		t.Fatalf("same version: %v", err)
	}
	info, _ = s.OrgDPA(ctx, owner, org.Slug, "en")
	if info.Current.ID != v.ID || info.Current.Body != "# DPA\n\nFinal text." || info.Accepted != nil || len(info.History) != 1 {
		t.Fatalf("new version: %+v", info)
	}
	if _, err := s.AcceptDPA(ctx, owner, org.Slug, in); invalidKey(err) != "validation.dpa_outdated" {
		t.Fatalf("outdated: %v", err)
	}
	in.VersionID, in.Lang = v.ID, "en"
	if acc2, err := s.AcceptDPA(ctx, owner, org.Slug, in); err != nil || acc2.Version != "2026-11" || acc2.Lang != "en" {
		t.Fatalf("accept new: %v %+v", err, acc2)
	}
	if a, _ := s.AdminDPA(ctx, admin); a.Placeholder || len(a.Versions) != 1 {
		t.Fatalf("admin: %+v", a)
	}
}
