package store

import (
	"context"
	"testing"
	"time"
)

// TestRoles checks the effective role SQL on both databases.
func TestRoles(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mk := func(email string) User {
				u, err := d.CreateUser(ctx, User{ID: id(), Email: email, Name: email})
				if err != nil {
					t.Fatal(err)
				}
				return u
			}
			owner, admin, grp, guest, docOnly, stranger := mk("o@x"), mk("a@x"), mk("g@x"), mk("gu@x"), mk("d@x"), mk("s@x")
			org, err := d.CreateOrg(ctx, Org{ID: id(), Name: "Firma", Slug: "firma", UsageType: "commercial"}, admin.ID)
			if err != nil {
				t.Fatal(err)
			}
			p, err := d.CreateProject(ctx, Project{ID: id(), Slug: "netz", Name: "Netz", Short: "NE", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: owner.ID, OrgID: org.ID})
			if err != nil {
				t.Fatal(err)
			}
			g := Group{ID: id(), OrgID: org.ID, Name: "IT"}
			_ = d.SaveGroup(ctx, g)
			_ = d.SetGroupMember(ctx, g.ID, grp.ID, "manual", true)
			_ = d.SetProjectGroup(ctx, p.ID, g.ID, RoleEditor)
			_ = d.SetMember(ctx, p.ID, guest.ID, RoleReader, Time{time.Now().Add(-time.Hour)})
			doc := Document{ID: id(), ProjectID: p.ID, Type: DocMarkdown, Slug: "a", Title: "A", Content: JSON(`{}`), CreatedBy: owner.ID}
			if _, err := d.CreateDocument(ctx, doc, "user:"+owner.ID); err != nil {
				t.Fatal(err)
			}
			_ = d.SetDocGrant(ctx, id(), doc.ID, docOnly.ID, "", RoleReader)
			for u, want := range map[string]string{owner.ID: RoleOwner, admin.ID: RoleOwner, grp.ID: RoleEditor, guest.ID: "", docOnly.ID: RoleDoc, stranger.ID: ""} {
				got, err := d.ProjectFor(ctx, p.Slug, u)
				if err != nil || got.Role != want || got.OrgName != "Firma" || !got.OrgAI {
					t.Errorf("%s: role %q want %q (%v)", u, got.Role, want, err)
				}
				ps, _ := d.ListProjects(ctx, u, false)
				if (len(ps) == 1) != (want != "") {
					t.Errorf("%s: list %d", u, len(ps))
				}
			}
			if r, _ := d.DocRoles(ctx, p.ID, docOnly.ID); r[doc.ID] != RoleReader {
				t.Errorf("doc roles: %v", r)
			}
			_ = d.SetRestricted(ctx, doc.ID, true)
			if r, _ := d.RestrictedDocs(ctx, []string{p.ID}); !r[doc.ID] {
				t.Errorf("restricted: %v", r)
			}
			l, err := d.CreateShareLink(ctx, ShareLink{ID: id(), ProjectID: p.ID, Label: "x", CreatedBy: owner.ID}, "hash1")
			if err != nil {
				t.Fatal(err)
			}
			if got, err := d.ShareLinkByHash(ctx, "hash1"); err != nil || got.ID != l.ID {
				t.Errorf("share link: %v", err)
			}
		})
	}
}
