package core

import (
	"context"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestThemeContrast(t *testing.T) {
	if r := ContrastRatio("#000000", "#ffffff"); r != 21 {
		t.Fatalf("black on white: %v", r)
	}
	// The built-in tokens are readable.
	if p := CheckThemeContrast(ThemeData{}); len(p) != 0 {
		t.Fatalf("defaults: %+v", p)
	}
	bad := ThemeData{Light: map[string]string{"fg": "#bbbbbb"}}
	if p := CheckThemeContrast(bad); len(p) == 0 || p[0].Mode != "light" {
		t.Fatalf("pale text: %+v", p)
	}
	// The high-contrast variant fixes every pair to 7:1.
	v := ContrastVariant(bad)
	for _, mode := range []string{"light", "dark"} {
		set := v.Light
		if mode == "dark" {
			set = v.Dark
		}
		pal := palette(mode, set)
		for _, pair := range contrastPairs {
			if r := ContrastRatio(pal[pair[0]], pal[pair[1]]); r < 7 {
				t.Fatalf("%s %s on %s: %v", mode, pair[0], pair[1], r)
			}
		}
	}
	if p := CheckThemeContrast(HighContrastTheme()); len(p) != 0 {
		t.Fatalf("built-in HC: %+v", p)
	}
}

func TestThemeValidation(t *testing.T) {
	ok := ThemeData{Light: map[string]string{"accent": "#0a7d5a"}, Body: ThemeFont{Family: "Inter"}, Radius: 8, Density: "compact"}
	if err := validTheme(&ok, ".card { border-width: 2px }"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		d   ThemeData
		css string
	}{
		"validation.theme_token":       {ThemeData{Light: map[string]string{"evil": "#000000"}}, ""},
		"validation.theme_color":       {ThemeData{Dark: map[string]string{"bg": "red; background:url(x)"}}, ""},
		"validation.theme_font":        {ThemeData{Body: ThemeFont{Family: "Comic Sans"}}, ""},
		"validation.theme_font_upload": {ThemeData{Body: ThemeFont{Family: "Mine", URL: "/api/v1/files/x"}}, ""},
		"validation.theme_radius":      {ThemeData{Radius: 99}, ""},
		"validation.theme_css":         {ThemeData{}, "@import url(https://evil.example/x.css);"},
		"validation.theme_css_braces":  {ThemeData{}, ".a { color: red"},
	}
	for key, c := range cases {
		if err := validTheme(&c.d, c.css); invalidKey(err) != key {
			t.Errorf("%s: %v", key, err)
		}
	}
	for _, css := range []string{"body{background:url(https://x/y.png)}", "a{content:'Pay here'}", "dialog{display:none}", "x{background:url(data:image/png;base64,AA)}"} {
		if err := validTheme(&ThemeData{}, css); invalidKey(err) != "validation.theme_css" {
			t.Errorf("css %q: %v", css, err)
		}
	}
}

func TestThemeResolution(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	s.Plugins.Version = "1.4.0"
	admin := Actor{User: store.User{ID: stranger.User.ID, Name: stranger.User.Name, IsAdmin: true}, Via: "web"}
	o, err := s.CreateOrg(ctx, admin, OrgInput{Name: "Firma", AdminEmail: "owner@x.de"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrgMember(ctx, owner, o.Slug, "reader@x.de", store.OrgMemberRole); err != nil {
		t.Fatal(err)
	}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Team", Org: &o.Slug})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	mk := func(a Actor, scope, ownerRef, name string) ThemeView {
		t.Helper()
		v, err := s.CreateTheme(ctx, a, ThemeInput{Scope: scope, Owner: ownerRef, Name: name, Data: ThemeData{Light: map[string]string{"accent": "#0a5d7d"}}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return v
	}
	// Permissions per scope.
	if _, err := s.CreateTheme(ctx, reader, ThemeInput{Scope: "project", Owner: p.Slug, Name: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers must not create project themes: %v", err)
	}
	if _, err := s.CreateTheme(ctx, reader, ThemeInput{Scope: "org", Owner: o.Slug, Name: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("members must not create org themes: %v", err)
	}
	if _, err := s.CreateTheme(ctx, owner, ThemeInput{Scope: "instance", Name: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("only system admins create instance themes: %v", err)
	}
	mine := mk(reader, "user", "", "Mine")
	if _, err := s.GetTheme(ctx, owner, mine.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("personal themes are private: %v", err)
	}
	inst := mk(admin, "instance", "", "Instance")
	orgT := mk(owner, "org", o.Slug, "Company")
	projT := mk(owner, "project", p.Slug, "Project")

	eff := func(a Actor, proj string) EffectiveTheme {
		t.Helper()
		c, err := s.Themes(ctx, a, proj)
		if err != nil {
			t.Fatal(err)
		}
		return c.Effective
	}
	id := func(e EffectiveTheme) string {
		if e.Theme == nil {
			return ""
		}
		return e.Theme.ID
	}
	if e := eff(reader, ""); e.Source != "builtin" {
		t.Fatalf("nothing chosen: %+v", e)
	}
	if err := s.SetInstanceTheme(ctx, admin, inst.ID); err != nil {
		t.Fatal(err)
	}
	if e := eff(reader, ""); e.Source != "instance" || id(e) != inst.ID {
		t.Fatalf("instance default: %+v", e)
	}
	if _, err := s.SetOrgTheme(ctx, owner, o.Slug, OrgThemeInput{ThemeID: orgT.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectTheme(ctx, reader, p.Slug, projT.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers must not set the project theme: %v", err)
	}
	ownMine := mk(owner, "user", "", "Owner's own")
	if err := s.SetProjectTheme(ctx, owner, p.Slug, ownMine.ID); invalidKey(err) != "validation.theme_scope" {
		t.Fatalf("personal themes are not for projects: %v", err)
	}
	if _, err := s.SetOrgTheme(ctx, owner, o.Slug, OrgThemeInput{ThemeID: projT.ID}); invalidKey(err) != "validation.theme_scope" {
		t.Fatalf("project themes are not for organizations: %v", err)
	}
	_ = s.SetProjectTheme(ctx, owner, p.Slug, projT.ID)
	if e := eff(reader, p.Slug); e.Source != "project" {
		t.Fatalf("project theme first: %+v", e)
	}
	if err := s.SetProjectTheme(ctx, owner, p.Slug, ""); err != nil {
		t.Fatal(err)
	}
	if e := eff(reader, p.Slug); e.Source != "org" || id(e) != orgT.ID {
		t.Fatalf("org default: %+v", e)
	}
	_ = s.SetProjectTheme(ctx, owner, p.Slug, projT.ID)
	if _, err := s.SetMyTheme(ctx, reader, MyThemeInput{ThemeID: mine.ID}); err != nil {
		t.Fatal(err)
	}
	if e := eff(reader, p.Slug); e.Source != "project" {
		t.Fatalf("project beats mine without always: %+v", e)
	}
	if e := eff(reader, ""); e.Source != "mine" {
		t.Fatalf("mine outside projects: %+v", e)
	}
	_, _ = s.SetMyTheme(ctx, reader, MyThemeInput{ThemeID: mine.ID, Always: true})
	if e := eff(reader, p.Slug); e.Source != "mine" {
		t.Fatalf("always mine: %+v", e)
	}
	// A forced organization theme wins, except the built-in high contrast.
	_, _ = s.SetOrgTheme(ctx, owner, o.Slug, OrgThemeInput{ThemeID: orgT.ID, Forced: true})
	if e := eff(reader, p.Slug); e.Source != "forced" || !e.Forced {
		t.Fatalf("forced: %+v", e)
	}
	_, _ = s.SetMyTheme(ctx, reader, MyThemeInput{ThemeID: mine.ID, Contrast: "high"})
	if e := eff(reader, p.Slug); e.Source != "contrast" {
		t.Fatalf("high contrast beats forced: %+v", e)
	}
	_, _ = s.SetMyTheme(ctx, reader, MyThemeInput{Contrast: "variant"})
	if e := eff(reader, p.Slug); e.Source != "forced" || len(CheckThemeContrast(e.Theme.Data)) != 0 || !e.Theme.Data.CSSOff {
		t.Fatalf("variant of the forced theme: %+v", e)
	}
	if _, err := s.SetMyTheme(ctx, owner, MyThemeInput{ThemeID: mine.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("others' personal themes cannot be chosen: %v", err)
	}
	// Deleting a theme clears its uses.
	if err := s.DeleteTheme(ctx, owner, orgT.ID); err != nil {
		t.Fatal(err)
	}
	if ot, _ := s.OrgThemeOf(ctx, owner, o.Slug); ot.ThemeID != "" || ot.Forced {
		t.Fatalf("org theme after delete: %+v", ot)
	}
}

func TestThemeCSSRelease(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.Plugins.Version = "1.4.0"
	v, err := s.CreateTheme(ctx, owner, ThemeInput{Scope: "user", Name: "Own", CSS: ".card { border-width: 2px }"})
	if err != nil || !v.CSSReleasedHere || v.CSSReleased != "1.4.0" {
		t.Fatalf("create: %v %+v", err, v)
	}
	s.Plugins.Version = "1.5.0"
	_, _ = s.SetMyTheme(ctx, owner, MyThemeInput{ThemeID: v.ID})
	c, _ := s.Themes(ctx, owner, "")
	if !c.Effective.CSSWarning || c.Effective.Theme.CSSReleasedHere {
		t.Fatalf("not released for 1.5.0: %+v", c.Effective)
	}
	r, err := s.ReleaseThemeCSS(ctx, owner, v.ID)
	if err != nil || !r.CSSReleasedHere || r.CSSReleased != "1.4.0,1.5.0" {
		t.Fatalf("release: %v %+v", err, r)
	}
	// Changed CSS is released only for the running version.
	u, err := s.UpdateTheme(ctx, owner, v.ID, ThemeInput{Name: "Own", CSS: ".card { border-width: 3px }"})
	if err != nil || u.CSSReleased != "1.5.0" {
		t.Fatalf("update: %v %+v", err, u)
	}
}
