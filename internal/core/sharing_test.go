package core

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/store"
)

func TestOrganizations(t *testing.T) {
	ctx := context.Background()
	s, owner, member, outsider := setup(t)
	admin := Actor{User: store.User{ID: owner.User.ID, Email: owner.User.Email, IsAdmin: true}, Via: "web"}

	if _, err := s.CreateOrg(ctx, member, OrgInput{Name: "Firma"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin create: %v", err)
	}
	org, err := s.CreateOrg(ctx, admin, OrgInput{Name: "Firma GmbH", AdminEmail: member.User.Email})
	if err != nil || org.Slug != "firma-gmbh" {
		t.Fatalf("create: %v %+v", err, org)
	}
	if err := s.RemoveOrgMember(ctx, member, org.Slug, member.User.ID); invalidKey(err) != "validation.last_org_admin" {
		t.Fatalf("last admin: %v", err)
	}
	if _, err := s.Org(ctx, outsider, org.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider sees org: %v", err)
	}
	if err := s.SetOrgMember(ctx, member, org.Slug, outsider.User.Email, store.OrgMemberRole); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateOrg(ctx, member, org.Slug, OrgInput{UsageType: "noncommercial"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("org admin must not change the usage type: %v", err)
	}

	// a project in the organization: the org admin is owner without membership
	p, err := s.CreateProject(ctx, outsider, ProjectInput{Name: "Netz", Org: &org.Slug, AIWriteMode: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Project(ctx, member, p.Slug); err != nil || got.Role != store.RoleOwner || got.OrgName != "Firma GmbH" {
		t.Fatalf("org admin role: %v %+v", err, got)
	}
	none := ""
	if _, err := s.CreateProject(ctx, owner, ProjectInput{Name: "X", Org: &org.Slug}); invalidKey(err) != "validation.org_unknown" {
		t.Fatalf("project in a foreign org: %v", err)
	}
	_ = none

	// groups give roles in projects of the organization
	g, err := s.SaveGroup(ctx, member, org.Slug, "", GroupInput{Name: "Technik"})
	if err != nil {
		t.Fatal(err)
	}
	third := mkUser(t, s, "third@x.de")
	if err := s.SetGroupMember(ctx, member, org.Slug, g.ID, "third@x.de", true); invalidKey(err) != "validation.not_org_member" {
		t.Fatalf("group needs org membership: %v", err)
	}
	_ = s.SetOrgMember(ctx, member, org.Slug, "third@x.de", store.OrgMemberRole)
	if err := s.SetGroupMember(ctx, member, org.Slug, g.ID, "third@x.de", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(ctx, third, p.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("org membership alone must not open projects: %v", err)
	}
	if err := s.SetProjectGroup(ctx, outsider, p.Slug, g.ID, store.RoleEditor); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Project(ctx, third, p.Slug); err != nil || got.Role != store.RoleEditor {
		t.Fatalf("group role: %v %+v", err, got)
	}

	// the organization switches AI off for its projects
	ai := outsider
	ai.Via = "mcp"
	if _, err := s.Documents(ctx, ai, p.Slug, store.DocFilter{}); err != nil {
		t.Fatalf("AI before: %v", err)
	}
	off := false
	if _, err := s.UpdateOrg(ctx, member, org.Slug, OrgInput{AIEnabled: &off}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Documents(ctx, ai, p.Slug, store.DocFilter{}); invalidKey(err) != "validation.ai_disabled" {
		t.Fatalf("AI after org switch: %v", err)
	}
	if ps, _ := s.Projects(ctx, ai); len(ps) != 0 {
		t.Fatalf("AI must not list org projects: %+v", ps)
	}

	// guests: access ends at the given time
	guest := mkUser(t, s, "gast@x.de")
	if err := s.SetMemberUntil(ctx, outsider, p.Slug, "gast@x.de", store.RoleReader, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(ctx, guest, p.Slug); err != nil {
		t.Fatalf("guest: %v", err)
	}
	_ = s.DB.SetMember(ctx, p.ID, guest.User.ID, store.RoleReader, store.Time{Time: time.Now().Add(-time.Minute)})
	if _, err := s.Project(ctx, guest, p.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired guest: %v", err)
	}
	if err := s.SetMemberUntil(ctx, outsider, p.Slug, "gast@x.de", store.RoleOwner, time.Now().Add(time.Hour)); invalidKey(err) != "validation.guest_until" {
		t.Fatalf("guest owner: %v", err)
	}
	if err := s.RemoveMember(ctx, outsider, p.Slug, outsider.User.ID); invalidKey(err) != "validation.last_owner" {
		t.Fatalf("last owner: %v", err)
	}
}

func TestDocumentSharing(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, outsider := setup(t)
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "f")}, filepath.Join(dir, "t")
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "direct"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	open, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "Router im Flur"})
	secret, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Zugangsdaten", Markdown: "Tresor im Keller"})
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "router", Name: "Router"})
	att, _ := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "tresor.txt", Document: secret.Document.Slug}, strings.NewReader("Kombination im Tresor"))

	if _, err := s.SetRestricted(ctx, reader, p.Slug, secret.Document.Slug, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader restricts: %v", err)
	}
	if _, err := s.SetRestricted(ctx, owner, p.Slug, secret.Document.Slug, true); err != nil {
		t.Fatal(err)
	}
	visible := func(a Actor) []string {
		ds, _ := s.Documents(ctx, a, p.Slug, store.DocFilter{})
		var out []string
		for _, d := range ds {
			out = append(out, d.Title)
		}
		return out
	}
	if got := visible(reader); len(got) != 1 || got[0] != "Netz" {
		t.Fatalf("reader list: %v", got)
	}
	if _, err := s.Document(ctx, reader, p.Slug, secret.Document.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reader get: %v", err)
	}
	if hits := mustSearch(t, s, reader, "tresor"); len(hits) != 0 {
		t.Fatalf("restricted page or its file in search: %+v", hits)
	}
	if _, err := s.OpenFile(ctx, reader, att.ID, 0, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("file of a restricted page: %v", err)
	}
	if len(visible(owner)) != 2 {
		t.Fatal("owner sees all")
	}

	// sharing with the reader opens it, as editor even
	if _, err := s.ShareDocument(ctx, owner, p.Slug, secret.Document.Slug, GrantInput{Email: reader.User.Email, Role: store.RoleEditor}); err != nil {
		t.Fatal(err)
	}
	if len(visible(reader)) != 2 {
		t.Fatal("grant must show the page")
	}
	if _, err := s.UpdateDocument(ctx, reader, p.Slug, secret.Document.Slug, DocumentPatch{Markdown: new("Tresor im Flur")}); err != nil {
		t.Fatalf("editor grant: %v", err)
	}

	// a person outside the project sees only the shared page
	if _, err := s.Project(ctx, outsider, p.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider before: %v", err)
	}
	_, _ = s.ShareDocument(ctx, owner, p.Slug, open.Document.Slug, GrantInput{Email: outsider.User.Email, Role: store.RoleReader})
	got, err := s.Project(ctx, outsider, p.Slug)
	if err != nil || got.Role != store.RoleDoc {
		t.Fatalf("outsider project: %v %+v", err, got)
	}
	if v := visible(outsider); len(v) != 1 || v[0] != "Netz" {
		t.Fatalf("outsider list: %v", v)
	}
	if _, err := s.Entities(ctx, outsider, p.Slug, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider entities: %v", err)
	}
	if _, err := s.UpdateDocument(ctx, outsider, p.Slug, open.Document.Slug, DocumentPatch{Markdown: new("x")}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider write: %v", err)
	}
	if hits := mustSearch(t, s, outsider, "router"); len(hits) != 1 || hits[0].Type != "document" {
		t.Fatalf("outsider search: %+v", hits)
	}
	sh, _ := s.Sharing(ctx, owner, p.Slug, open.Document.Slug)
	if _, err := s.UnshareDocument(ctx, owner, p.Slug, open.Document.Slug, sh.Grants[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(ctx, outsider, p.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider after unshare: %v", err)
	}
}

func TestShareLinks(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "f")}, filepath.Join(dir, "t")
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "direct"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	netz, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "Router"})
	geheim, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Geheim", Markdown: "x"})
	_, _ = s.SetRestricted(ctx, owner, p.Slug, geheim.Document.Slug, true)
	img, _ := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "plan.png", Document: netz.Document.Slug}, strings.NewReader("png"))

	if _, err := s.CreateShareLink(ctx, reader, p.Slug, ShareLinkInput{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader link: %v", err)
	}
	link, err := s.CreateShareLink(ctx, owner, p.Slug, ShareLinkInput{Label: "Hausmeister", Days: 7})
	if err != nil || !strings.HasPrefix(link.Token, "phxs_") || link.ExpiresAt.IsZero() {
		t.Fatalf("link: %v %+v", err, link)
	}
	v, err := s.Shared(ctx, link.Token)
	if err != nil || v.Project.Name != "Haus" || len(v.Documents) != 1 || v.Documents[0].Title != "Netz" || v.Single {
		t.Fatalf("shared view: %v %+v", err, v)
	}
	if d, err := s.SharedDocument(ctx, link.Token, netz.Document.Slug); err != nil || !strings.Contains(string(d.Content), "Router") {
		t.Fatalf("shared doc: %v", err)
	}
	if _, err := s.SharedDocument(ctx, link.Token, geheim.Document.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restricted via link: %v", err)
	}
	fc, err := s.SharedFile(ctx, link.Token, img.ID, false)
	if err != nil {
		t.Fatalf("shared file: %v", err)
	}
	b, _ := io.ReadAll(fc.Reader)
	fc.Reader.Close()
	if string(b) != "png" {
		t.Fatal("file content")
	}
	if _, err := s.Shared(ctx, "phxs_wrong"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong token: %v", err)
	}
	// a link for one page
	one, err := s.CreateShareLink(ctx, owner, p.Slug, ShareLinkInput{Document: geheim.Document.Slug})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Shared(ctx, one.Token); !v.Single || len(v.Documents) != 1 || v.Documents[0].Title != "Geheim" {
		t.Fatalf("single: %+v", v)
	}
	if ls, _ := s.ShareLinks(ctx, owner, p.Slug); len(ls) != 2 {
		t.Fatalf("links: %d", len(ls))
	}
	if err := s.RevokeShareLink(ctx, owner, p.Slug, link.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Shared(ctx, link.Token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked: %v", err)
	}
}

func mkUser(t *testing.T, s *Service, email string) Actor {
	t.Helper()
	u, err := s.DB.CreateUser(context.Background(), store.User{ID: newID(), Email: email, Name: email})
	if err != nil {
		t.Fatal(err)
	}
	return Actor{User: u, Via: "web"}
}
