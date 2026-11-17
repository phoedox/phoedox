package core

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Themes outside the app: publications use the theme of their (first
// themed) project or its organization; the sign-in page of an
// organization's own domain shows the organization theme's logo.

// themeForProject is the project's theme, else its organization's.
func (s *Service) themeForProject(ctx context.Context, p store.Project) (store.Theme, bool) {
	if id, _ := s.DB.ProjectThemeID(ctx, p.ID); id != "" {
		if t, err := s.DB.GetTheme(ctx, id); err == nil {
			return t, true
		}
	}
	if p.OrgID != "" {
		if id, _, _ := s.DB.OrgTheme(ctx, p.OrgID); id != "" {
			if t, err := s.DB.GetTheme(ctx, id); err == nil {
				return t, true
			}
		}
	}
	return store.Theme{}, false
}

// PubTheme is the CSS (tokens for light and dark, uploaded fonts, shape)
// and logo a publication takes from its projects' theme.
func (s *Service) PubTheme(ctx context.Context, pub store.Publication) (css, logo string) {
	for _, id := range pub.Projects {
		p, err := s.DB.ProjectFor(ctx, id, "")
		if err != nil {
			continue
		}
		t, ok := s.themeForProject(ctx, p)
		if !ok {
			continue
		}
		d := themeData(t)
		// Publications may live on their own domain: absolute addresses.
		abs := func(u string) string {
			if strings.HasPrefix(u, "/api/v1/theme-assets/") {
				return s.PublicURL + u
			}
			return u
		}
		for _, f := range []*ThemeFont{&d.Body, &d.Display, &d.Mono} {
			f.URL = abs(f.URL)
		}
		return themeVarsCSS(d), publicLogo(abs(d.Logo))
	}
	return "", ""
}

// themeVarsCSS renders a theme as custom properties (no own CSS: that
// belongs to the app's markup).
func themeVarsCSS(d ThemeData) string {
	vars := func(m map[string]string) string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			if hexRe.MatchString(m[k]) {
				fmt.Fprintf(&b, "--%s:%s;", k, m[k])
			}
		}
		return b.String()
	}
	var b strings.Builder
	for _, f := range []ThemeFont{d.Body, d.Display, d.Mono} {
		if f.URL != "" && f.Family != "" && !strings.ContainsAny(f.Family+f.URL, `"'<>{}\`) {
			fmt.Fprintf(&b, `@font-face{font-family:"%s";src:url("%s") format("woff2");font-display:swap}`, f.Family, f.URL)
		}
	}
	root := vars(palette("light", d.Light))
	stack := func(f ThemeFont, fallback string) string {
		if f.Family == "" || strings.ContainsAny(f.Family, `"'<>{};\`) {
			return ""
		}
		return fmt.Sprintf(`"%s", %s`, f.Family, fallback)
	}
	if v := stack(d.Body, "system-ui, sans-serif"); v != "" {
		root += "--font-body:" + v + ";"
	}
	if v := stack(d.Display, "system-ui, sans-serif"); v != "" {
		root += "--font-display:" + v + ";"
	}
	if v := stack(d.Mono, "ui-monospace, monospace"); v != "" {
		root += "--font-mono:" + v + ";"
	}
	if d.Radius > 0 {
		root += fmt.Sprintf("--radius:%dpx;--radius-card:%dpx;", d.Radius, min(d.Radius+4, 28))
	}
	fmt.Fprintf(&b, ":root{%s}@media (prefers-color-scheme: dark){:root{%s}}", root, vars(palette("dark", d.Dark)))
	return b.String()
}

// publicLogo keeps logos anyone can load (theme assets or https).
func publicLogo(u string) string {
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "/api/v1/theme-assets/") || strings.Contains(u, "/api/v1/theme-assets/") {
		return u
	}
	return ""
}
