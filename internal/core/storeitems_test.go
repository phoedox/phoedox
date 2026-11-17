package core

import (
	"context"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/plugins"
	"github.com/phoedox/phoedox/internal/store"
)

func itemPackage(t *testing.T, files map[string]string) []byte {
	t.Helper()
	m := map[string][]byte{}
	for k, v := range files {
		m[k] = []byte(v)
	}
	data, err := plugins.BuildPackage(m)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStoreThemesAndApps(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger, priv := pluginSetup(t)
	s.SyncNotify = true
	theme := func(v, accent string) []byte {
		return itemPackage(t, map[string]string{
			plugins.ManifestFile: `{"type":"theme","id":"de.example.forest","name":"Forest","version":"` + v + `","engines":{"phoedox":">=1.0.0"}}`,
			"theme.json":         `{"light":{"accent":"` + accent + `"},"dark":{},"body":{"family":"Inter","url":"https://evil.example/x.woff2"}}`,
			"theme.css":          `.sidebar { letter-spacing: .01em; }`,
		})
	}
	app := itemPackage(t, map[string]string{
		plugins.ManifestFile: `{"type":"app","id":"de.example.chores","name":"Chores","version":"1.0.0","engines":{"phoedox":">=1.0.0"}}`,
		"app.json":           `{"level":2,"storageMb":5}`,
		"files/index.html":   `<h1>Chores</h1>`,
	})
	fs := &fakeStore{priv: priv, pkgs: map[string][]byte{
		"de.example.forest@1.0.0": theme("1.0.0", "#2c6e49"), "de.example.forest@1.1.0": theme("1.1.0", "#1f5236"),
		"de.example.chores@1.0.0": app,
	}, items: []CatalogEntry{{Type: "theme", ID: "de.example.forest", Name: "Forest", Version: "1.1.0"}, {Type: "app", ID: "de.example.chores", Name: "Chores", Version: "1.0.0"}}}
	s.Plugins.Store = fs

	// A personal theme from the store, then the update notice and the update.
	v, err := s.InstallStoreTheme(ctx, owner, StoreThemeInput{ID: "de.example.forest", Version: "1.0.0", Scope: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if v.OriginVersion != "1.0.0" || v.Data.Light["accent"] != "#2c6e49" || v.Data.Body.URL != "" || !v.CSSReleasedHere {
		t.Fatalf("installed theme: %+v", v)
	}
	cat, err := s.StoreCatalog(ctx, owner, "theme", "")
	if err != nil || len(cat.Items) != 1 || len(cat.Items[0].InstalledIn) != 1 || cat.Items[0].InstalledIn[0].Update != "1.1.0" {
		t.Fatalf("catalog: %v %+v", err, cat)
	}
	if v2, err := s.InstallStoreTheme(ctx, owner, StoreThemeInput{ID: "de.example.forest", Version: "1.1.0", Theme: v.ID}); err != nil || v2.ID != v.ID || v2.OriginVersion != "1.1.0" {
		t.Fatalf("update: %v %+v", err, v2)
	}
	// Installing again into the same owner replaces the theme (rollback).
	if v3, err := s.InstallStoreTheme(ctx, owner, StoreThemeInput{ID: "de.example.forest", Version: "1.0.0", Scope: "user"}); err != nil || v3.ID != v.ID || v3.OriginVersion != "1.0.0" {
		t.Fatalf("rollback: %v %+v", err, v3)
	}
	if vs, _ := s.StoreVersions(ctx, owner, "de.example.forest"); len(vs) != 2 {
		t.Fatalf("versions: %+v", vs)
	}
	if _, err := s.InstallStoreTheme(ctx, reader, StoreThemeInput{ID: "de.example.forest", Version: "1.0.0", Theme: v.ID}); err == nil {
		t.Fatal("others must not update a person's theme")
	}
	// A wrong type is refused.
	if _, err := s.InstallStoreTheme(ctx, owner, StoreThemeInput{ID: "de.example.chores", Version: "1.0.0", Scope: "user"}); err == nil {
		t.Fatal("an app is not a theme")
	}
	if _, err := s.InstallPlugin(ctx, Actor{User: store.User{ID: owner.User.ID, IsAdmin: true}}, app, nil, "upload"); err == nil {
		t.Fatal("an app template is not a plugin")
	}

	// An organization that lets nobody but admins install app templates.
	admin := Actor{User: store.User{ID: stranger.User.ID, Name: stranger.User.Name, IsAdmin: true}, Via: "web"}
	o, err := s.CreateOrg(ctx, admin, OrgInput{Name: "Firma", AdminEmail: "owner@x.de"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrgMember(ctx, owner, o.Slug, "reader@x.de", store.OrgMemberRole); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProject(ctx, reader, ProjectInput{Name: "Team", Org: &o.Slug})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.InstallStoreApp(ctx, reader, p.Slug, StoreAppInput{ID: "de.example.chores", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetApp(ctx, reader, p.Slug, res.Document.ID)
	if got.App.Origin == nil || got.App.Origin.Version != "1.0.0" || got.App.Level != 2 || got.App.Files["index.html"] == "" {
		t.Fatalf("app: %+v", got.App)
	}
	// Edits keep the origin.
	if _, err := s.EditApp(ctx, reader, p.Slug, res.Document.ID, nil, []AppEdit{{File: "index.html", Old: "Chores", New: "Jobs"}}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetApp(ctx, reader, p.Slug, res.Document.ID); got.App.Origin == nil {
		t.Fatal("origin lost on edit")
	}
	apps, _ := s.StoreCatalog(ctx, reader, "app", p.Slug)
	if apps.Access != "yes" || len(apps.Items[0].InstalledIn) != 1 {
		t.Fatalf("apps: %+v", apps)
	}

	if _, err := s.SetOrgStoreAccess(ctx, reader, o.Slug, StoreAccess{Apps: StoreRule{Who: "none"}}); err == nil {
		t.Fatal("members must not change store access")
	}
	g, err := s.SaveGroup(ctx, owner, o.Slug, "", GroupInput{Name: "Builders"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetOrgStoreAccess(ctx, owner, o.Slug, StoreAccess{Apps: StoreRule{Who: "groups", Groups: []string{g.ID, "unknown"}}}); err != nil {
		t.Fatal(err)
	}
	if acc, _ := s.OrgStoreAccess(ctx, owner, o.Slug); len(acc.Apps.Groups) != 1 {
		t.Fatalf("unknown groups must be dropped: %+v", acc)
	}
	if _, err := s.InstallStoreApp(ctx, reader, p.Slug, StoreAppInput{ID: "de.example.chores", Version: "1.0.0"}); err == nil {
		t.Fatal("not in the group")
	}
	// The member asks; the admin approves and the app is created.
	r, err := s.RequestStoreItem(ctx, reader, p.Slug, StoreRequestInput{Type: "app", ID: "de.example.chores", Version: "1.0.0", Name: "Chores", Title: "Household jobs", Note: "for the family"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestStoreItem(ctx, reader, p.Slug, StoreRequestInput{Type: "app", ID: "de.example.chores", Version: "1.0.0", Name: "Chores"}); err == nil {
		t.Fatal("one open request per item")
	}
	if _, err := s.DecideStoreRequest(ctx, reader, o.Slug, r.ID, StoreDecision{Approve: true}); err == nil {
		t.Fatal("members must not decide")
	}
	done, err := s.DecideStoreRequest(ctx, owner, o.Slug, r.ID, StoreDecision{Approve: true})
	if err != nil || done.Status != "approved" || done.Result == "" {
		t.Fatalf("approve: %v %+v", err, done)
	}
	if _, err := s.Document(ctx, reader, p.Slug, done.Result); err != nil {
		t.Fatalf("approved app: %v", err)
	}
	if mine, _ := s.MyStoreRequests(ctx, reader, p.Slug); len(mine) != 1 || mine[0].Status != "approved" {
		t.Fatalf("my requests: %+v", mine)
	}
	// Group members may install; requests can be switched off.
	if err := s.SetGroupMember(ctx, owner, o.Slug, g.ID, "reader@x.de", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InstallStoreApp(ctx, reader, p.Slug, StoreAppInput{ID: "de.example.chores", Version: "1.0.0"}); err != nil {
		t.Fatalf("group member: %v", err)
	}
	_, _ = s.SetOrgStoreAccess(ctx, owner, o.Slug, StoreAccess{Themes: StoreRule{Who: "none"}, NoRequests: true})
	if _, err := s.RequestStoreItem(ctx, reader, p.Slug, StoreRequestInput{Type: "theme", ID: "de.example.forest", Version: "1.1.0"}); err == nil {
		t.Fatal("requests are off")
	}
	if c, _ := s.StoreCatalog(ctx, reader, "theme", p.Slug); c.Access != "no" {
		t.Fatalf("access: %+v", c.Access)
	}
	if c, _ := s.StoreCatalog(ctx, owner, "theme", p.Slug); c.Access != "yes" {
		t.Fatalf("org admins: %+v", c.Access)
	}
	// Without a store.
	s.Plugins.Store = nil
	if c, err := s.StoreCatalog(ctx, owner, "theme", ""); err != nil || !c.Off {
		t.Fatalf("off: %v %+v", err, c)
	}
	if _, err := s.InstallStoreTheme(ctx, owner, StoreThemeInput{ID: "x.y", Version: "1.0.0", Scope: "user"}); err == nil || errors.Is(err, ErrForbidden) {
		t.Fatalf("store off: %v", err)
	}
}
