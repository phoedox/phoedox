package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// UI themes (ui-themes-concept): a theme is a token set for light and dark
// (base and component tokens), fonts, shape and optional own CSS that is
// released per Phoedox version. Themes belong to a person, a project, an
// organization or the instance; the effective theme follows: forced
// organization theme → the person's ("always mine") → the project's → the
// person's → the organization's default → the instance's default.

// ThemeTokens are the tokens a theme may set, by mode.
var ThemeTokens = []string{
	"bg", "surface", "surface2", "line", "fg", "muted", "accent", "accent-soft", "signal", "bad", "good", "wire", "on-color",
	"btn-bg", "btn-fg", "btn-ghost-bg", "btn-ghost-fg", "input-bg", "input-border", "sidebar-bg", "header-bg", "link", "focus",
}

// ThemeFonts are the built-in, freely licensed fonts a theme can pick.
var ThemeFonts = []string{"IBM Plex Sans", "Bricolage Grotesque", "Inter", "Source Sans 3", "Atkinson Hyperlegible", "Lexend", "Nunito", "Merriweather", "IBM Plex Mono", "JetBrains Mono", "system-ui"}

// ThemeFont is a font of a theme: a built-in one by name, or an uploaded
// woff2 file of the owner with a licence note.
type ThemeFont struct {
	Family  string `json:"family"`
	URL     string `json:"url,omitempty"`     // /api/v1/files/… for uploaded fonts
	License string `json:"license,omitempty"` // licence note for uploaded fonts
}

// ThemeData is the content of a theme.
type ThemeData struct {
	Light   map[string]string `json:"light"`
	Dark    map[string]string `json:"dark"`
	Body    ThemeFont         `json:"body"`
	Display ThemeFont         `json:"display"`
	Mono    ThemeFont         `json:"mono"`
	Radius  int               `json:"radius"`  // px, 0 = default
	Density string            `json:"density"` // compact | normal | comfortable
	Logo    string            `json:"logo,omitempty"`
	// CSSOff switches the own CSS off (admins, e.g. until it is released for
	// the running version).
	CSSOff bool `json:"cssOff,omitempty"`
}

var defaultTokens = map[string]map[string]string{
	"light": {"bg": "#f6f4f2", "surface": "#ffffff", "surface2": "#eeeae6", "line": "#ddd5ce", "fg": "#1f1a17", "muted": "#6b6159", "accent": "#b84a0c", "accent-soft": "#fbe4d4",
		"signal": "#9a7200", "bad": "#b4372f", "good": "#2c8a4b", "wire": "#3a5bb8", "on-color": "#ffffff"},
	"dark": {"bg": "#14110f", "surface": "#1c1815", "surface2": "#26201c", "line": "#3a312a", "fg": "#eee7e1", "muted": "#a69b92", "accent": "#f26a1b", "accent-soft": "#3d2416",
		"signal": "#e2bf4f", "bad": "#e2675d", "good": "#5bc27b", "wire": "#7f9cf0", "on-color": "#ffffff"},
}

// component tokens fall back to base tokens.
var tokenFallback = map[string]string{"btn-bg": "accent", "btn-fg": "surface", "btn-ghost-bg": "surface", "btn-ghost-fg": "fg", "input-bg": "surface", "input-border": "line",
	"sidebar-bg": "bg", "header-bg": "surface", "link": "accent", "focus": "accent"}

// palette is a mode's effective colours: defaults, the theme's values and
// the fallbacks of component tokens.
func palette(mode string, set map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range defaultTokens[mode] {
		out[k] = v
	}
	for k, v := range set {
		out[k] = v
	}
	for k, base := range tokenFallback {
		if out[k] == "" {
			out[k] = out[base]
		}
	}
	return out
}

var hexRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func luminance(hex string) float64 {
	ch := func(i int) float64 {
		v, _ := strconv.ParseUint(hex[i:i+2], 16, 8)
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(1) + 0.7152*ch(3) + 0.0722*ch(5)
}

// ContrastRatio is the WCAG contrast of two #rrggbb colours.
func ContrastRatio(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return math.Round((la+0.05)/(lb+0.05)*100) / 100
}

// ThemeProblem is a pair of colours that is not readable enough.
type ThemeProblem struct {
	Mode  string  `json:"mode"`
	Fg    string  `json:"fg"`
	Bg    string  `json:"bg"`
	Ratio float64 `json:"ratio"`
	Need  float64 `json:"need"`
}

var contrastPairs = [][3]string{{"fg", "bg", "4.5"}, {"fg", "surface", "4.5"}, {"muted", "surface", "4.5"}, {"muted", "bg", "4.5"}, {"btn-fg", "btn-bg", "4.5"},
	{"btn-ghost-fg", "btn-ghost-bg", "4.5"}, {"fg", "input-bg", "4.5"}, {"link", "surface", "4.5"}, {"bad", "surface", "4.5"}, {"fg", "sidebar-bg", "4.5"}, {"fg", "header-bg", "4.5"}}

// CheckThemeContrast lists the pairs below WCAG AA in both modes.
func CheckThemeContrast(d ThemeData) []ThemeProblem {
	out := []ThemeProblem{}
	for _, mode := range []string{"light", "dark"} {
		set := d.Light
		if mode == "dark" {
			set = d.Dark
		}
		p := palette(mode, set)
		for _, pair := range contrastPairs {
			need, _ := strconv.ParseFloat(pair[2], 64)
			if r := ContrastRatio(p[pair[0]], p[pair[1]]); r < need {
				out = append(out, ThemeProblem{Mode: mode, Fg: pair[0], Bg: pair[1], Ratio: r, Need: need})
			}
		}
	}
	return out
}

var cssForbidden = []*regexp.Regexp{
	regexp.MustCompile(`(?i)@import`),
	regexp.MustCompile(`(?i)url\(\s*['"]?\s*(?:https?:|//|data:|javascript:)`),
	regexp.MustCompile(`(?i)expression\s*\(`),
	regexp.MustCompile(`(?i)content\s*:\s*['"][^'"]+['"]`),
	regexp.MustCompile(`(?i)-moz-binding|behavior\s*:`),
	regexp.MustCompile(`(?i)</?style`),
	// Dialogs that confirm something stay as Phoedox draws them.
	regexp.MustCompile(`(?i)dialog|\.confirm|phx-proposals|phx-confirm`),
}

func validTheme(d *ThemeData, css string) error {
	for _, mode := range []map[string]string{d.Light, d.Dark} {
		for k, v := range mode {
			if !slices.Contains(ThemeTokens, k) {
				return invalid("validation.theme_token", map[string]any{"token": k}, "unknown token %q", k)
			}
			if !hexRe.MatchString(v) {
				return invalid("validation.theme_color", map[string]any{"token": k}, "colours must be #rrggbb (%s)", k)
			}
		}
	}
	for _, f := range []*ThemeFont{&d.Body, &d.Display, &d.Mono} {
		if f.Family == "" {
			continue
		}
		if f.URL == "" && !slices.Contains(ThemeFonts, f.Family) {
			return invalid("validation.theme_font", map[string]any{"font": f.Family}, "unknown font %q", f.Family)
		}
		if f.URL != "" {
			if !strings.HasPrefix(f.URL, "/api/v1/") || strings.TrimSpace(f.License) == "" {
				return invalid("validation.theme_font_upload", map[string]any{"font": f.Family}, "uploaded fonts need a file of the project and a licence note")
			}
			if strings.ContainsAny(f.Family, `"';{}<>`) {
				return invalid("validation.theme_font", map[string]any{"font": f.Family}, "invalid font name")
			}
		}
	}
	if d.Radius < 0 || d.Radius > 24 {
		return invalid("validation.theme_radius", nil, "the corner radius must be 0–24 px")
	}
	if d.Density != "" && !slices.Contains([]string{"compact", "normal", "comfortable"}, d.Density) {
		return invalid("validation.theme_density", nil, "unknown density")
	}
	if d.Logo != "" && !strings.HasPrefix(d.Logo, "https://") && !strings.HasPrefix(d.Logo, "/api/v1/") {
		return invalid("validation.feedback_logo", nil, "the logo must be an https address or a file")
	}
	if len(css) > 40_000 {
		return invalid("validation.theme_css_size", nil, "the CSS is too long")
	}
	for _, re := range cssForbidden {
		if m := re.FindString(css); m != "" {
			return invalid("validation.theme_css", map[string]any{"rule": m}, "this CSS is not allowed: %s", m)
		}
	}
	if strings.Count(css, "{") != strings.Count(css, "}") {
		return invalid("validation.theme_css_braces", nil, "the CSS braces do not match")
	}
	return nil
}

// mix moves a colour towards black (t<0) or white (t>0) by |t|.
func mix(hex string, t float64) string {
	ch := func(i int) float64 {
		v, _ := strconv.ParseUint(hex[i:i+2], 16, 8)
		c := float64(v)
		if t > 0 {
			return c + (255-c)*t
		}
		return c * (1 + t)
	}
	return fmt.Sprintf("#%02x%02x%02x", int(math.Round(ch(1))), int(math.Round(ch(3))), int(math.Round(ch(5))))
}

// ContrastVariant is a theme's high-contrast variant: every text pair is
// pushed to at least 7:1 (WCAG AAA) while keeping the theme's hues.
func ContrastVariant(d ThemeData) ThemeData {
	out := d
	for _, mode := range []string{"light", "dark"} {
		set := d.Light
		dir := -1.0 // text gets darker in light mode
		if mode == "dark" {
			set, dir = d.Dark, 1.0
		}
		p := palette(mode, set)
		// Backgrounds move to the extreme, text away from it.
		for _, bg := range []string{"bg", "surface", "input-bg", "sidebar-bg", "header-bg"} {
			p[bg] = mix(p[bg], -dir*0.6)
		}
		p["line"] = mix(p["line"], dir*0.5)
		for _, pair := range contrastPairs {
			fg := pair[0]
			for i := 0; i < 20 && ContrastRatio(p[fg], p[pair[1]]) < 7; i++ {
				if pair[0] == "btn-fg" {
					// Buttons: darken (light) / lighten (dark) the button instead.
					p[pair[1]] = mix(p[pair[1]], dir*0.15)
					continue
				}
				p[fg] = mix(p[fg], dir*0.2)
			}
		}
		if mode == "light" {
			out.Light = p
		} else {
			out.Dark = p
		}
	}
	out.CSSOff = true // own CSS may undo the contrast
	return out
}

// HighContrastTheme is the built-in high-contrast theme that people can
// always choose, even when their organization forces a theme.
func HighContrastTheme() ThemeData {
	return ThemeData{
		Light: map[string]string{"bg": "#ffffff", "surface": "#ffffff", "surface2": "#ececec", "line": "#000000", "fg": "#000000", "muted": "#1f1f1f", "accent": "#8a2c00", "accent-soft": "#ffe0cc",
			"signal": "#5c4300", "bad": "#8f0000", "good": "#005a1c", "wire": "#002f9e", "on-color": "#ffffff", "btn-bg": "#000000", "btn-fg": "#ffffff", "link": "#00268a", "focus": "#000000"},
		Dark: map[string]string{"bg": "#000000", "surface": "#000000", "surface2": "#1a1a1a", "line": "#ffffff", "fg": "#ffffff", "muted": "#e6e6e6", "accent": "#ffb27f", "accent-soft": "#3a1d0a",
			"signal": "#ffe07a", "bad": "#ff9b94", "good": "#8cf0a8", "wire": "#a9c0ff", "on-color": "#000000", "btn-bg": "#ffffff", "btn-fg": "#000000", "link": "#a9c8ff", "focus": "#ffffff"},
		Radius: 4,
	}
}

// ThemeInput creates or changes a theme.
type ThemeInput struct {
	Scope string    `json:"scope"`           // user | project | org | instance
	Owner string    `json:"owner,omitempty"` // project or organization slug
	Name  string    `json:"name"`
	Data  ThemeData `json:"data"`
	CSS   string    `json:"css"`
}

// ThemeView is a theme with what the viewer may do and its CSS state.
type ThemeView struct {
	store.Theme
	Data      ThemeData `json:"data"`
	OwnerName string    `json:"ownerName,omitempty"`
	CanEdit   bool      `json:"canEdit"`
	// CSSReleasedHere: the own CSS is released for the running version.
	CSSReleasedHere bool `json:"cssReleasedHere"`
}

func themeData(t store.Theme) ThemeData {
	var d ThemeData
	_ = json.Unmarshal(t.Data, &d)
	if d.Light == nil {
		d.Light = map[string]string{}
	}
	if d.Dark == nil {
		d.Dark = map[string]string{}
	}
	return d
}

func (s *Service) releasedHere(t store.Theme) bool {
	return t.CSS == "" || slices.Contains(strings.Split(t.CSSReleased, ","), s.appVersion())
}

// themeOwner resolves the owner of a scope and whether the actor may edit.
func (s *Service) themeOwner(ctx context.Context, a Actor, scope, owner string, edit bool) (string, string, error) {
	switch scope {
	case "user":
		return a.User.ID, a.User.Name, nil
	case "project":
		need := store.RoleReader
		if edit {
			need = store.RoleOwner
		}
		p, err := s.project(ctx, a, owner, need)
		return p.ID, p.Name, err
	case "org":
		need := store.OrgMemberRole
		if edit {
			need = store.OrgAdmin
		}
		o, err := s.org(ctx, a, owner, need)
		return o.ID, o.Name, err
	case "instance":
		if edit && !a.User.IsAdmin {
			return "", "", ErrForbidden
		}
		return "", "", nil
	}
	return "", "", invalid("validation.theme_scope", nil, "unknown scope %q", scope)
}

// themeAccess reads a theme the actor may see (and edit when asked).
func (s *Service) themeAccess(ctx context.Context, a Actor, id string, edit bool) (store.Theme, string, error) {
	t, err := s.DB.GetTheme(ctx, id)
	if err != nil {
		return t, "", err
	}
	if t.Scope == "user" && t.OwnerID != a.User.ID {
		return t, "", ErrNotFound
	}
	_, name, err := s.themeOwner(ctx, a, t.Scope, t.OwnerID, edit)
	return t, name, err
}

func (s *Service) themeView(a Actor, t store.Theme, ownerName string, canEdit bool) ThemeView {
	return ThemeView{Theme: t, Data: themeData(t), OwnerName: ownerName, CanEdit: canEdit, CSSReleasedHere: s.releasedHere(t)}
}

// CreateTheme stores a new theme.
func (s *Service) CreateTheme(ctx context.Context, a Actor, in ThemeInput) (ThemeView, error) {
	if err := s.fullUser(a); err != nil {
		return ThemeView{}, err
	}
	owner, name, err := s.themeOwner(ctx, a, in.Scope, in.Owner, true)
	if err != nil {
		return ThemeView{}, err
	}
	if err := validTheme(&in.Data, in.CSS); err != nil {
		return ThemeView{}, err
	}
	title := strings.TrimSpace(in.Name)
	if title == "" {
		return ThemeView{}, invalid("validation.title_required", nil, "name is required")
	}
	raw, _ := json.Marshal(in.Data)
	now := store.Now()
	t := store.Theme{ID: newID(), Scope: in.Scope, OwnerID: owner, Name: excerpt(title, 80), Data: raw, CSS: in.CSS, CreatedBy: a.User.ID, CreatedAt: now, UpdatedAt: now}
	if in.CSS != "" {
		t.CSSReleased = s.appVersion()
	}
	if err := s.DB.SaveTheme(ctx, t); err != nil {
		return ThemeView{}, err
	}
	s.event(ctx, a, "theme.create", t.ID, map[string]string{"scope": t.Scope, "name": t.Name})
	return s.themeView(a, t, name, true), nil
}

// UpdateTheme changes a theme; saved CSS counts as released for the
// running version.
func (s *Service) UpdateTheme(ctx context.Context, a Actor, id string, in ThemeInput) (ThemeView, error) {
	t, name, err := s.themeAccess(ctx, a, id, true)
	if err != nil {
		return ThemeView{}, err
	}
	if err := validTheme(&in.Data, in.CSS); err != nil {
		return ThemeView{}, err
	}
	if n := strings.TrimSpace(in.Name); n != "" {
		t.Name = excerpt(n, 80)
	}
	raw, _ := json.Marshal(in.Data)
	t.Data, t.UpdatedAt = raw, store.Now()
	if in.CSS != t.CSS {
		t.CSS, t.CSSReleased = in.CSS, ""
	}
	if t.CSS != "" && !s.releasedHere(t) && !strings.Contains(t.CSSReleased, s.appVersion()) {
		t.CSSReleased = strings.Trim(t.CSSReleased+","+s.appVersion(), ",")
	}
	if err := s.DB.SaveTheme(ctx, t); err != nil {
		return ThemeView{}, err
	}
	s.event(ctx, a, "theme.update", t.ID, map[string]string{"name": t.Name})
	return s.themeView(a, t, name, true), nil
}

// DeleteTheme removes a theme.
func (s *Service) DeleteTheme(ctx context.Context, a Actor, id string) error {
	t, _, err := s.themeAccess(ctx, a, id, true)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteTheme(ctx, t.ID); err != nil {
		return err
	}
	s.event(ctx, a, "theme.delete", t.ID, nil)
	return nil
}

// GetTheme reads a theme.
func (s *Service) GetTheme(ctx context.Context, a Actor, id string) (ThemeView, error) {
	t, name, err := s.themeAccess(ctx, a, id, false)
	if err != nil {
		return ThemeView{}, err
	}
	_, _, editErr := s.themeAccess(ctx, a, id, true)
	return s.themeView(a, t, name, editErr == nil), nil
}

// ThemeSummary is a theme in a list.
type ThemeSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Scope     string `json:"scope"`
	OwnerName string `json:"ownerName,omitempty"`
	// Owner is the slug of the project or organization.
	Owner   string `json:"owner,omitempty"`
	CanEdit bool   `json:"canEdit"`
	// OriginID and OriginVersion: the store item the theme came from.
	OriginID      string `json:"originId,omitempty"`
	OriginVersion string `json:"originVersion,omitempty"`
	// Swatch: bg, surface, fg, accent in light and dark (for the picker).
	Swatch []string `json:"swatch"`
}

func swatch(d ThemeData) []string {
	l, k := palette("light", d.Light), palette("dark", d.Dark)
	return []string{l["bg"], l["surface"], l["fg"], l["accent"], k["bg"], k["surface"], k["fg"], k["accent"]}
}

// EffectiveTheme is the theme in use, where it comes from and whether the
// person can change it.
type EffectiveTheme struct {
	Theme  *ThemeView `json:"theme"`
	Source string     `json:"source"` // forced | mine | project | org | instance | builtin
	Forced bool       `json:"forced"`
	// CSSWarning: the theme's own CSS is not released for this version.
	CSSWarning bool `json:"cssWarning,omitempty"`
}

// ThemeCatalog is what the appearance settings and the app need.
type ThemeCatalog struct {
	Available []ThemeSummary               `json:"available"`
	Mine      store.UserTheme              `json:"mine"`
	Effective EffectiveTheme               `json:"effective"`
	Fonts     []string                     `json:"fonts"`
	Tokens    []string                     `json:"tokens"`
	Defaults  map[string]map[string]string `json:"defaults"`
	// ProjectTheme is the chosen theme of the project asked for.
	ProjectTheme string `json:"projectTheme,omitempty"`
	// InstanceTheme is the instance's default (system admins).
	InstanceTheme string `json:"instanceTheme,omitempty"`
	// Builtin are the swatches of Phoedox's own theme and the high-contrast theme.
	Builtin map[string][]string `json:"builtin"`
	Version string              `json:"version"`
}

func (s *Service) themesOf(ctx context.Context, scope, ownerID, ownerSlug, ownerName string, canEdit bool, out *[]ThemeSummary) {
	ts, err := s.DB.Themes(ctx, scope, ownerID)
	if err != nil {
		return
	}
	for _, t := range ts {
		*out = append(*out, ThemeSummary{ID: t.ID, Name: t.Name, Scope: scope, OwnerName: ownerName, Owner: ownerSlug, CanEdit: canEdit, OriginID: t.OriginID, OriginVersion: t.OriginVersion, Swatch: swatch(themeData(t))})
	}
}

// Themes lists the themes the person can choose (and, with a project,
// the project's themes), with the effective theme.
func (s *Service) Themes(ctx context.Context, a Actor, projectRef string) (ThemeCatalog, error) {
	out := ThemeCatalog{Available: []ThemeSummary{}, Fonts: ThemeFonts, Tokens: ThemeTokens, Defaults: defaultTokens, Version: s.appVersion(),
		Builtin: map[string][]string{"phoedox": swatch(ThemeData{}), "contrast": swatch(HighContrastTheme())}}
	if a.User.ID == "" {
		return out, ErrForbidden
	}
	s.themesOf(ctx, "instance", "", "", "", a.User.IsAdmin, &out.Available)
	s.themesOf(ctx, "user", a.User.ID, "", a.User.Name, true, &out.Available)
	if orgs, err := s.DB.ListOrgs(ctx, a.User.ID, false); err == nil {
		for _, o := range orgs {
			s.themesOf(ctx, "org", o.ID, o.Slug, o.Name, o.Role == store.OrgAdmin, &out.Available)
		}
	}
	if projectRef != "" {
		if p, err := s.project(ctx, a, projectRef, store.RoleReader); err == nil {
			s.themesOf(ctx, "project", p.ID, p.Slug, p.Name, p.Role == store.RoleOwner, &out.Available)
			out.ProjectTheme, _ = s.DB.ProjectThemeID(ctx, p.ID)
		}
	}
	if a.User.IsAdmin {
		out.InstanceTheme, _ = s.DB.Setting(ctx, settingThemeDefault, "")
	}
	out.Mine, _ = s.DB.GetUserTheme(ctx, a.User.ID)
	out.Effective = s.effectiveTheme(ctx, a, projectRef, out.Mine)
	return out, nil
}

func (s *Service) loadThemeView(ctx context.Context, a Actor, id string) *ThemeView {
	if id == "" {
		return nil
	}
	t, err := s.DB.GetTheme(ctx, id)
	if err != nil {
		return nil
	}
	v := s.themeView(a, t, "", false)
	return &v
}

func (s *Service) effectiveTheme(ctx context.Context, a Actor, projectRef string, mine store.UserTheme) EffectiveTheme {
	if mine.Contrast == "high" {
		return EffectiveTheme{Theme: &ThemeView{Theme: store.Theme{ID: "builtin-hc", Scope: "builtin", Name: "High contrast"}, Data: HighContrastTheme(), CSSReleasedHere: true}, Source: "contrast"}
	}
	e := s.resolveTheme(ctx, a, projectRef, mine)
	if mine.Contrast == "variant" {
		if e.Theme == nil {
			e.Theme = &ThemeView{Theme: store.Theme{ID: "builtin", Scope: "builtin", Name: "Phoedox"}, Data: ThemeData{Light: map[string]string{}, Dark: map[string]string{}}, CSSReleasedHere: true}
		}
		v := *e.Theme
		v.Data = ContrastVariant(v.Data)
		v.CSS, v.CSSReleasedHere = "", true
		e.Theme, e.CSSWarning = &v, false
	}
	return e
}

func (s *Service) resolveTheme(ctx context.Context, a Actor, projectRef string, mine store.UserTheme) EffectiveTheme {
	var proj *store.Project
	if projectRef != "" {
		if p, err := s.project(ctx, a, projectRef, store.RoleReader); err == nil {
			proj = &p
		}
	}
	pick := func(id, source string, forced bool) (EffectiveTheme, bool) {
		v := s.loadThemeView(ctx, a, id)
		if v == nil {
			return EffectiveTheme{}, false
		}
		return EffectiveTheme{Theme: v, Source: source, Forced: forced, CSSWarning: v.CSS != "" && !v.CSSReleasedHere}, true
	}
	var orgTheme string
	if proj != nil && proj.OrgID != "" {
		id, forced, err := s.DB.OrgTheme(ctx, proj.OrgID)
		if err == nil && forced {
			if e, ok := pick(id, "forced", true); ok {
				return e
			}
		}
		orgTheme = id
	}
	if mine.Always {
		if e, ok := pick(mine.ThemeID, "mine", false); ok {
			return e
		}
	}
	if proj != nil {
		if id, err := s.DB.ProjectThemeID(ctx, proj.ID); err == nil {
			if e, ok := pick(id, "project", false); ok {
				return e
			}
		}
	}
	if e, ok := pick(mine.ThemeID, "mine", false); ok {
		return e
	}
	if e, ok := pick(orgTheme, "org", false); ok {
		return e
	}
	if id, _ := s.DB.Setting(ctx, settingThemeDefault, ""); id != "" {
		if e, ok := pick(id, "instance", false); ok {
			return e
		}
	}
	return EffectiveTheme{Source: "builtin"}
}

const settingThemeDefault = "theme.default"

// MyThemeInput is the person's theme choice.
type MyThemeInput = store.UserTheme

// SetMyTheme stores the person's theme choice.
func (s *Service) SetMyTheme(ctx context.Context, a Actor, in MyThemeInput) (store.UserTheme, error) {
	if err := s.fullUser(a); err != nil {
		return in, err
	}
	if in.ThemeID != "" {
		if _, _, err := s.themeAccess(ctx, a, in.ThemeID, false); err != nil {
			return in, err
		}
	}
	if !slices.Contains([]string{"", "high", "variant"}, in.Contrast) {
		return in, invalid("validation.theme_contrast_mode", nil, "unknown contrast mode")
	}
	return in, s.DB.SetUserTheme(ctx, a.User.ID, in)
}

// SetProjectTheme chooses a project's theme (owners).
func (s *Service) SetProjectTheme(ctx context.Context, a Actor, ref, themeID string) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if themeID != "" {
		t, _, err := s.themeAccess(ctx, a, themeID, false)
		if err != nil {
			return err
		}
		if t.Scope == "user" || (t.Scope == "project" && t.OwnerID != p.ID) {
			return invalid("validation.theme_scope", nil, "a project can use its own, its organization's or the instance's themes")
		}
	}
	s.event(ctx, a, "project.theme", p.ID, map[string]string{"theme": themeID})
	return s.DB.SetProjectTheme(ctx, p.ID, themeID)
}

// OrgThemeInput sets an organization's default theme.
type OrgThemeInput struct {
	ThemeID string `json:"themeId"`
	Forced  bool   `json:"forced"`
}

// SetOrgTheme sets an organization's default theme (organization admins).
func (s *Service) SetOrgTheme(ctx context.Context, a Actor, ref string, in OrgThemeInput) (OrgThemeInput, error) {
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return in, err
	}
	if in.ThemeID != "" {
		t, _, err := s.themeAccess(ctx, a, in.ThemeID, false)
		if err != nil {
			return in, err
		}
		if t.Scope != "instance" && (t.Scope != "org" || t.OwnerID != o.ID) {
			return in, invalid("validation.theme_scope", nil, "an organization can use its own or the instance's themes")
		}
	}
	s.event(ctx, a, "org.theme", o.ID, in)
	return in, s.DB.SetOrgTheme(ctx, o.ID, in.ThemeID, in.Forced && in.ThemeID != "")
}

// OrgThemeOf reads an organization's default theme (its members).
func (s *Service) OrgThemeOf(ctx context.Context, a Actor, ref string) (OrgThemeInput, error) {
	o, err := s.org(ctx, a, ref, store.OrgMemberRole)
	if err != nil {
		return OrgThemeInput{}, err
	}
	id, forced, err := s.DB.OrgTheme(ctx, o.ID)
	return OrgThemeInput{ThemeID: id, Forced: forced}, err
}

// SetInstanceTheme sets the instance's default theme (system admins).
func (s *Service) SetInstanceTheme(ctx context.Context, a Actor, themeID string) error {
	if !a.User.IsAdmin || a.Via == "mcp" {
		return ErrForbidden
	}
	if themeID != "" {
		t, err := s.DB.GetTheme(ctx, themeID)
		if err != nil || t.Scope != "instance" {
			return invalid("validation.theme_scope", nil, "the instance default must be an instance theme")
		}
	}
	s.event(ctx, a, "admin.theme", themeID, nil)
	return s.DB.SetSetting(ctx, settingThemeDefault, themeID, a.String())
}

// ThemeCheck is the result of checking a theme while editing.
type ThemeCheck struct {
	Problems []ThemeProblem `json:"problems"`
	Error    string         `json:"error,omitempty"` // i18n key of the first other problem
	Params   map[string]any `json:"params,omitempty"`
}

// CheckTheme checks a theme without saving it.
func (s *Service) CheckTheme(ctx context.Context, a Actor, in ThemeInput) ThemeCheck {
	out := ThemeCheck{Problems: CheckThemeContrast(in.Data)}
	if err := validTheme(&in.Data, in.CSS); err != nil {
		var inv *InvalidError
		if errors.As(err, &inv) && inv.Key != "validation.theme_contrast" {
			out.Error, out.Params = inv.Key, inv.Params
		}
	}
	sort.Slice(out.Problems, func(i, j int) bool { return out.Problems[i].Ratio < out.Problems[j].Ratio })
	return out
}

// appVersion is the running Phoedox version (themes release their CSS per
// version).
func (s *Service) appVersion() string {
	if s.Plugins.Version == "" {
		return "dev"
	}
	return s.Plugins.Version
}

// ReleaseThemeCSS marks a theme's own CSS as working with the running
// Phoedox version (its editors, after checking it).
func (s *Service) ReleaseThemeCSS(ctx context.Context, a Actor, id string) (ThemeView, error) {
	t, name, err := s.themeAccess(ctx, a, id, true)
	if err != nil {
		return ThemeView{}, err
	}
	if !s.releasedHere(t) {
		t.CSSReleased = strings.Trim(t.CSSReleased+","+s.appVersion(), ",")
		t.UpdatedAt = store.Now()
		if err := s.DB.SaveTheme(ctx, t); err != nil {
			return ThemeView{}, err
		}
		s.event(ctx, a, "theme.release", t.ID, map[string]string{"version": s.appVersion()})
	}
	return s.themeView(a, t, name, true), nil
}
