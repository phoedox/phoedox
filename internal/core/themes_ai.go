package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Themes made by the AI (ui-themes-concept): from a description and,
// optionally, the colours of a website. Runs on the person's own AI
// budget; the result is a proposal the editor shows, not a saved theme.

// ThemeIdea asks the AI for a theme.
type ThemeIdea struct {
	Description string `json:"description"`
	// URL of a website whose colours the theme should take up.
	URL string `json:"url,omitempty"`
}

// ThemeProposal is the AI's theme, already checked and made readable.
type ThemeProposal struct {
	Name   string    `json:"name"`
	Data   ThemeData `json:"data"`
	Colors []string  `json:"colors,omitempty"` // colours found on the website
}

// personalAsk calls the instance's default model for a person outside a
// project and charges their own monthly budget.
func (s *Service) personalAsk(ctx context.Context, a Actor, job string, system []string, prompt string, maxTokens int) (string, error) {
	if err := s.fullUser(a); err != nil {
		return "", err
	}
	st := s.aiChatSettings(ctx)
	ms := s.instanceModels(ctx)
	if len(ms) == 0 || !st.Enabled || !s.AIEnabled(ctx) {
		return "", invalid("ai_chat.unavailable.not_configured", nil, "no AI is set up")
	}
	since := monthStart(time.Now())
	if st.UserBudget > 0 {
		used, _ := s.DB.AIUsageSince(ctx, "", a.User.ID, since, false)
		if centsOf(used) >= int64(st.UserBudget) {
			return "", invalid("ai_chat.unavailable.user_budget", nil, "your AI budget is used up")
		}
	}
	m := ms[0]
	if want, _ := s.DB.Setting(ctx, settingAIChatModel, ""); want != "" {
		for _, x := range ms {
			if x.Ref == want {
				m = x
			}
		}
	}
	client, err := s.aiClient(m.source)
	if err != nil {
		return "", err
	}
	turn, err := client.Stream(ctx, ai.Request{Model: m.Model, System: system, MaxTokens: maxTokens, History: []ai.Message{{Raw: ai.UserMessage(prompt, nil)}}}, func(ai.Event) {})
	if err != nil {
		return "", invalid("validation.app_ai_failed", nil, "the AI did not answer")
	}
	s.RecordAIUsage(ctx, a, ChatStatus{}, job, m, turn.Usage)
	var b strings.Builder
	for _, p := range ai.Parts(turn.Message) {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String(), nil
}

var (
	hexColorRe  = regexp.MustCompile(`#[0-9a-fA-F]{6}\b`)
	themeMetaRe = regexp.MustCompile(`(?i)<meta[^>]+name=["']theme-color["'][^>]+content=["'](#[0-9a-fA-F]{6})`)
	cssLinkRe   = regexp.MustCompile(`(?i)<link[^>]+rel=["']stylesheet["'][^>]+href=["']([^"']+)`)
)

// siteColors reads the most used colours of a website (its HTML, theme
// color and first stylesheet), through the request policy.
func (s *Service) siteColors(ctx context.Context, raw string) ([]string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, invalid("validation.link_url", nil, "the address must be an http(s) URL")
	}
	get := func(addr string) (string, error) {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(cctx, http.MethodGet, addr, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", "Phoedox")
		res, err := s.RequestPolicy.Client().Do(req)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		b, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		return string(b), err
	}
	page, err := get(u.String())
	if err != nil {
		return nil, invalid("validation.theme_site", map[string]any{"error": err.Error()}, "the website could not be read: %v", err)
	}
	text := page
	if m := cssLinkRe.FindStringSubmatch(page); m != nil {
		if ref, err := u.Parse(m[1]); err == nil {
			if css, err := get(ref.String()); err == nil {
				text += css
			}
		}
	}
	count := map[string]int{}
	for _, c := range hexColorRe.FindAllString(text, -1) {
		c = strings.ToLower(c)
		if c != "#ffffff" && c != "#000000" {
			count[c]++
		}
	}
	colors := make([]string, 0, len(count))
	for c := range count {
		colors = append(colors, c)
	}
	sort.Slice(colors, func(i, j int) bool { return count[colors[i]] > count[colors[j]] })
	if m := themeMetaRe.FindStringSubmatch(page); m != nil {
		colors = append([]string{strings.ToLower(m[1])}, colors...)
	}
	return colors[:min(len(colors), 12)], nil
}

// readable pushes text tokens until every pair reaches WCAG AA.
func readable(d ThemeData) ThemeData {
	for _, mode := range []string{"light", "dark"} {
		set := d.Light
		if mode == "dark" {
			set = d.Dark
		}
		for range 3 {
			for _, p := range CheckThemeContrast(d) {
				if p.Mode != mode {
					continue
				}
				pal := palette(mode, set)
				dir := -1.0
				if luminance(pal[p.Bg]) < 0.35 {
					dir = 1.0
				}
				c := pal[p.Fg]
				for i := 0; i < 20 && ContrastRatio(c, pal[p.Bg]) < p.Need; i++ {
					c = mix(c, dir*0.15)
				}
				set[p.Fg] = c
			}
		}
	}
	return d
}

// GenerateTheme lets the AI make a theme proposal.
func (s *Service) GenerateTheme(ctx context.Context, a Actor, in ThemeIdea) (ThemeProposal, error) {
	in.Description = strings.TrimSpace(in.Description)
	if in.Description == "" && in.URL == "" {
		return ThemeProposal{}, invalid("validation.theme_idea", nil, "describe the theme or give a website")
	}
	var out ThemeProposal
	prompt := "Description: " + excerpt(in.Description, 2000)
	if in.URL != "" {
		colors, err := s.siteColors(ctx, in.URL)
		if err != nil {
			return out, err
		}
		out.Colors = colors
		prompt += fmt.Sprintf("\nColours of the website %s, most used first: %s", in.URL, strings.Join(colors, ", "))
	}
	system := strings.NewReplacer("{{tokens}}", strings.Join(ThemeTokens, ", "), "{{fonts}}", strings.Join(ThemeFonts, ", ")).Replace(resources.Prompt("theme-ai"))
	text, err := s.personalAsk(ctx, a, "theme:"+store.Now().Format("2006-01-02"), []string{system}, prompt, 1500)
	if err != nil {
		return out, err
	}
	var raw struct {
		Name    string            `json:"name"`
		Light   map[string]string `json:"light"`
		Dark    map[string]string `json:"dark"`
		Body    string            `json:"body"`
		Display string            `json:"display"`
		Mono    string            `json:"mono"`
		Radius  int               `json:"radius"`
		Density string            `json:"density"`
	}
	if i, j := strings.Index(text, "{"), strings.LastIndex(text, "}"); i >= 0 && j > i {
		text = text[i : j+1]
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return out, invalid("validation.theme_ai_answer", nil, "the AI's answer was not a theme")
	}
	keep := func(set map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range set {
			if hexRe.MatchString(v) && inList(ThemeTokens, k) {
				m[k] = strings.ToLower(v)
			}
		}
		return m
	}
	font := func(f string) ThemeFont {
		if inList(ThemeFonts, f) {
			return ThemeFont{Family: f}
		}
		return ThemeFont{}
	}
	d := ThemeData{Light: keep(raw.Light), Dark: keep(raw.Dark), Body: font(raw.Body), Display: font(raw.Display), Mono: font(raw.Mono),
		Radius: max(0, min(raw.Radius, 24)), Density: raw.Density}
	if !inList([]string{"compact", "comfortable"}, d.Density) {
		d.Density = ""
	}
	out.Name = excerpt(strings.TrimSpace(raw.Name), 80)
	out.Data = readable(d)
	return out, nil
}

func inList(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
