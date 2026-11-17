package core

import (
	"context"
	"encoding/json"

	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/blocks"
	"github.com/phoedox/phoedox/internal/store"
)

// Whiteboards (ADR-0068): a canvas of shapes, lines, text, sticky notes,
// frames, images, embeds and entries, drawn together live. Unlike pages
// and floor plans there is no draft: changes are published automatically
// (saves of the same person within liveWindow update one version) and
// everyone on the board sees them at once.

// Whiteboard is the content of a whiteboard document.
type Whiteboard struct {
	Version    int         `json:"version"`
	Background string      `json:"background,omitempty"` // plain | grid | dots
	Elements   []WBElement `json:"elements"`
	Comments   []WBComment `json:"comments,omitempty"`
	// Prototypes: clickable screens drawn on the board (prototypes.go).
	Prototypes []WBPrototype `json:"prototypes,omitempty"`
}

// WBElement is one element of a whiteboard. Boxy elements (shapes, text,
// sticky notes, frames, images, embeds, entries) use x/y/w/h; lines,
// arrows and freehand strokes use points in board coordinates.
type WBElement struct {
	ID   string  `json:"id"`
	Type string  `json:"type"`
	X    float64 `json:"x,omitempty"`
	Y    float64 `json:"y,omitempty"`
	W    float64 `json:"w,omitempty"`
	H    float64 `json:"h,omitempty"`
	// Order sorts elements back to front (a fractional index, so inserts
	// from several people keep their order).
	Order  string       `json:"order,omitempty"`
	Points [][2]float64 `json:"points,omitempty"`
	Text   string       `json:"text,omitempty"`
	// Style: palette colours, stroke width, dashes, hand-drawn look.
	Stroke   string  `json:"stroke,omitempty"`
	Fill     string  `json:"fill,omitempty"`
	Width    float64 `json:"width,omitempty"`
	Dash     string  `json:"dash,omitempty"`   // "" | dashed | dotted
	Smooth   bool    `json:"smooth,omitempty"` // true: clean lines instead of hand-drawn
	Opacity  float64 `json:"opacity,omitempty"`
	FontSize float64 `json:"fontSize,omitempty"`
	Align    string  `json:"align,omitempty"` // left | center | right
	// Lines and arrows: round corners through the waypoints, and the shapes
	// the ends are bound to.
	Round bool       `json:"round,omitempty"`
	Start *WBBinding `json:"start,omitempty"`
	End   *WBBinding `json:"end,omitempty"`
	// Structure.
	Frame  string   `json:"frame,omitempty"`
	Groups []string `json:"groups,omitempty"`
	Locked bool     `json:"locked,omitempty"`
	Seed   int      `json:"seed,omitempty"`
	// Links: an entry (type "entity" or any shape) or a page.
	Entity string `json:"entity,omitempty"`
	Doc    string `json:"doc,omitempty"`
	// Images: a project file. Embeds: the address, its kind and a preview.
	File    string     `json:"file,omitempty"`
	URL     string     `json:"url,omitempty"`
	Kind    string     `json:"kind,omitempty"` // video | code | link | web | app (Doc is the app)
	Preview *WBPreview `json:"preview,omitempty"`
	// Blocks (ADR-0085): type "block" with the block's kind in Kind and the
	// same source as the fenced block in a page. Chart is the older form of a
	// chart block (type "chart"), still read.
	Source string          `json:"source,omitempty"`
	Chart  json.RawMessage `json:"chart,omitempty"`
	// Prototypes: a frame as a screen, a component (type "ui") and what
	// happens on click, change, hover or when a screen opens.
	Screen  *WBScreen  `json:"screen,omitempty"`
	UI      *WBUI      `json:"ui,omitempty"`
	Actions []WBAction `json:"actions,omitempty"`
}

// WBBinding binds the end of a line or arrow to an element: towards its
// centre, or (with FX/FY, 0–1 of its box) to a fixed point of its outline.
type WBBinding struct {
	Element string   `json:"element"`
	FX      *float64 `json:"fx,omitempty"`
	FY      *float64 `json:"fy,omitempty"`
}

// WBPreview describes an embed: title, description, site and for code
// links their state (open, merged, closed).
type WBPreview struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Site        string `json:"site,omitempty"`
	Image       string `json:"image,omitempty"`
	Status      string `json:"status,omitempty"`
	// Frameable reports whether the site allows being shown in a frame.
	Frameable bool `json:"frameable,omitempty"`
}

// WBComment is a comment pin on a spot or an element; Ink holds
// handwritten strokes (each a list of points in a 110×40 box).
type WBComment struct {
	ID       string         `json:"id"`
	X        float64        `json:"x"`
	Y        float64        `json:"y"`
	Element  string         `json:"element,omitempty"`
	Author   string         `json:"author,omitempty"`
	Name     string         `json:"name,omitempty"`
	Text     string         `json:"text,omitempty"`
	Ink      [][][2]float64 `json:"ink,omitempty"`
	At       string         `json:"at,omitempty"`
	Resolved bool           `json:"resolved,omitempty"`
	Replies  []WBReply      `json:"replies,omitempty"`
}

// WBReply answers a comment.
type WBReply struct {
	ID     string `json:"id"`
	Author string `json:"author,omitempty"`
	Name   string `json:"name,omitempty"`
	Text   string `json:"text"`
	At     string `json:"at,omitempty"`
}

const (
	maxWBElements = 5000
	maxWBComments = 500
	maxWBBytes    = 2 << 20
	maxWBCoord    = 1e7
	maxWBPoints   = 5000
	maxWBText     = 20_000
	// liveWindow: saves of the same person within it update one version.
	liveWindow = 10 * time.Minute
)

var (
	wbTypes = map[string]bool{"rect": true, "ellipse": true, "diamond": true, "line": true, "arrow": true, "pen": true, "text": true,
		"sticky": true, "frame": true, "image": true, "embed": true, "entity": true, "ui": true, "chart": true, "block": true}
	wbLinear   = map[string]bool{"line": true, "arrow": true, "pen": true}
	wbEmbeds   = map[string]bool{"video": true, "code": true, "link": true, "web": true, "app": true}
	wbColors   = map[string]bool{"gray": true, "red": true, "orange": true, "yellow": true, "green": true, "blue": true, "purple": true, "pink": true}
	wbOrderRe  = regexp.MustCompile(`^[0-9A-Za-z]{0,40}$`)
	wbFileIDRe = regexp.MustCompile(`^[0-9A-Z]{26}$`)
)

func badBoard(what string) error {
	return invalid("validation.whiteboard", map[string]any{"what": what}, "whiteboard: invalid %s", what)
}

// whiteboardContent validates a whiteboard and returns its JSON and the
// text for search and mentions (texts, embed titles, comments, entries).
func whiteboardContent(wb *Whiteboard) (store.JSON, string, error) {
	if wb == nil {
		wb = &Whiteboard{}
	}
	wb.Version = 1
	if wb.Elements == nil {
		wb.Elements = []WBElement{}
	}
	switch wb.Background {
	case "", "plain", "grid", "dots":
	default:
		return nil, "", badBoard("background")
	}
	if len(wb.Elements) > maxWBElements {
		return nil, "", badBoard("count")
	}
	if len(wb.Comments) > maxWBComments {
		return nil, "", badBoard("comments")
	}
	ok := func(v ...float64) bool {
		for _, x := range v {
			if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) > maxWBCoord {
				return false
			}
		}
		return true
	}
	ids := map[string]bool{}
	for _, e := range wb.Elements {
		if !planIDRe.MatchString(e.ID) || ids[e.ID] {
			return nil, "", badBoard("id")
		}
		ids[e.ID] = true
	}
	var text []string
	for i := range wb.Elements {
		e := &wb.Elements[i]
		bad := func(what string) error { return badBoard(fmt.Sprintf("%s (%s)", what, e.ID)) }
		if !wbTypes[e.Type] {
			return nil, "", bad("type")
		}
		if !ok(e.X, e.Y, e.W, e.H, e.Width, e.FontSize) || e.W < 0 || e.H < 0 || e.Width < 0 || e.Width > 40 || e.FontSize < 0 || e.FontSize > 400 {
			return nil, "", bad("geometry")
		}
		if e.Opacity < 0 || e.Opacity > 1 || math.IsNaN(e.Opacity) {
			return nil, "", bad("opacity")
		}
		if !wbOrderRe.MatchString(e.Order) {
			return nil, "", bad("order")
		}
		if (e.Stroke != "" && !wbColors[e.Stroke]) || (e.Fill != "" && !wbColors[e.Fill]) {
			return nil, "", bad("colour")
		}
		switch e.Dash {
		case "", "dashed", "dotted":
		default:
			return nil, "", bad("dash")
		}
		switch e.Align {
		case "", "left", "center", "right":
		default:
			return nil, "", bad("align")
		}
		if len(e.Text) > maxWBText {
			return nil, "", bad("text")
		}
		switch {
		case e.Type == "block" || e.Type == "chart":
			kind, src := e.Kind, []byte(e.Source)
			if e.Type == "chart" {
				kind, src = "chart", e.Chart
			}
			d, ok := blocks.Get(kind)
			if !ok || !d.Board {
				return nil, "", invalid("validation.whiteboard_block_kind", map[string]any{"id": e.ID, "kind": kind}, "whiteboard: %s cannot be a block on a whiteboard", kind)
			}
			if ps := errorsOnly(blocks.Check(kind, src)); len(ps) > 0 {
				return nil, "", invalid("validation.whiteboard_chart", map[string]any{"id": e.ID, "problem": ps[0].Message}, "whiteboard: block %s: %s", e.ID, ps[0].Message)
			}
			if d.Text != nil {
				text = append(text, d.Text(src))
			}
		case len(e.Chart) > 0 || e.Source != "":
			return nil, "", bad("source")
		}
		if wbLinear[e.Type] {
			if len(e.Points) < 2 || len(e.Points) > maxWBPoints {
				return nil, "", bad("points")
			}
			for _, p := range e.Points {
				if !ok(p[0], p[1]) {
					return nil, "", bad("points")
				}
			}
		} else if len(e.Points) > 0 {
			return nil, "", bad("points")
		}
		for _, b := range []*WBBinding{e.Start, e.End} {
			if b == nil {
				continue
			}
			if e.Type == "pen" || b.Element == e.ID || !ids[b.Element] {
				return nil, "", bad("binding")
			}
			if (b.FX == nil) != (b.FY == nil) || (b.FX != nil && (!ok(*b.FX, *b.FY) || *b.FX < -0.5 || *b.FX > 1.5 || *b.FY < -0.5 || *b.FY > 1.5)) {
				return nil, "", bad("binding")
			}
		}
		if e.Frame != "" && (!ids[e.Frame] || e.Frame == e.ID) {
			return nil, "", bad("frame")
		}
		if len(e.Groups) > 10 {
			return nil, "", bad("groups")
		}
		for _, g := range e.Groups {
			if !planIDRe.MatchString(g) {
				return nil, "", bad("groups")
			}
		}
		if e.Type == "image" && !wbFileIDRe.MatchString(e.File) {
			return nil, "", bad("file")
		}
		if e.Type != "image" && e.File != "" {
			return nil, "", bad("file")
		}
		if e.Type == "embed" && e.Kind == "app" {
			// An app of the project: it runs with the viewer's rights.
			if e.Doc == "" || e.URL != "" {
				return nil, "", bad("link")
			}
		} else if e.Type == "embed" {
			u, err := url.Parse(e.URL)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(e.URL) > 2000 {
				return nil, "", bad("url")
			}
			if !wbEmbeds[e.Kind] {
				return nil, "", bad("kind")
			}
			if e.Preview != nil {
				pv := e.Preview
				if len(pv.Title) > 500 || len(pv.Description) > 2000 || len(pv.Site) > 200 || len(pv.Status) > 40 || (pv.Image != "" && !wbFileIDRe.MatchString(pv.Image)) {
					return nil, "", bad("preview")
				}
				text = append(text, pv.Title, pv.Description)
			}
		} else if e.URL != "" || (e.Kind != "" && e.Type != "block") || e.Preview != nil {
			return nil, "", bad("url")
		}
		if e.Type == "entity" && e.Entity == "" {
			return nil, "", bad("entity")
		}
		if len(e.Entity) > 200 || len(e.Doc) > 200 {
			return nil, "", bad("link")
		}
		if err := checkPrototypeElement(wb, e, ids); err != nil {
			return nil, "", err
		}
		if e.Text != "" {
			text = append(text, e.Text)
		}
		if e.UI != nil {
			text = append(text, e.UI.Placeholder, strings.Join(e.UI.Options, " "))
		}
		if e.Entity != "" {
			text = append(text, "@"+e.Entity)
		}
		if e.Doc != "" {
			text = append(text, "[["+e.Doc+"]]")
		}
	}
	pt, err := checkPrototypes(wb)
	if err != nil {
		return nil, "", err
	}
	text = append(text, pt...)
	cids := map[string]bool{}
	for i := range wb.Comments {
		c := &wb.Comments[i]
		if !planIDRe.MatchString(c.ID) || cids[c.ID] || !ok(c.X, c.Y) || len(c.Text) > maxWBText || len(c.Name) > 200 || len(c.Author) > 200 || len(c.Replies) > 200 {
			return nil, "", badBoard("comment")
		}
		cids[c.ID] = true
		if c.Element != "" && !ids[c.Element] {
			c.Element = "" // the element is gone: the pin stays where it is
		}
		if len(c.Ink) > 100 {
			return nil, "", badBoard("comment")
		}
		for _, s := range c.Ink {
			if len(s) > 1000 {
				return nil, "", badBoard("comment")
			}
			for _, p := range s {
				if !ok(p[0], p[1]) {
					return nil, "", badBoard("comment")
				}
			}
		}
		text = append(text, c.Text)
		for _, r := range c.Replies {
			if len(r.Text) > maxWBText || !planIDRe.MatchString(r.ID) {
				return nil, "", badBoard("comment")
			}
			text = append(text, r.Text)
		}
	}
	b, err := json.Marshal(wb)
	if err != nil {
		return nil, "", err
	}
	if len(b) > maxWBBytes {
		return nil, "", badBoard("size")
	}
	return b, strings.Join(text, "\n"), nil
}

// wbFiles checks that images and embed pictures are files of the project.
func (s *Service) wbFiles(ctx context.Context, p store.Project, wb *Whiteboard) error {
	if wb == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, e := range wb.Elements {
		ids := []string{e.File}
		if e.Preview != nil {
			ids = append(ids, e.Preview.Image)
		}
		for _, id := range ids {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			f, err := s.DB.GetFile(ctx, id)
			if err != nil || f.ProjectID != p.ID {
				return badBoard("file " + id)
			}
		}
	}
	return nil
}

// SaveWhiteboard stores the board from the live editor: published at once,
// one version per person and liveWindow. Editors on the board already have
// the change, so the live session is not reset.
func (s *Service) SaveWhiteboard(ctx context.Context, a Actor, ref, docRef string, wb *Whiteboard) (store.Document, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return store.Document{}, err
	}
	if doc.Type != store.DocWhiteboard || a.Via != "web" {
		return store.Document{}, ErrForbidden
	}
	if s.needsApproval(ctx, p) {
		return store.Document{}, invalid("validation.approval_required", nil, "an owner has to approve this change")
	}
	if err := s.wbFiles(ctx, p, wb); err != nil {
		return store.Document{}, err
	}
	c, text, err := whiteboardContent(wb)
	if err != nil {
		return store.Document{}, err
	}
	if sameJSON(c, doc.Content) {
		return doc, nil
	}
	doc.Content = c
	saved, created, err := s.DB.SaveLive(ctx, doc, a.String(), liveWindow)
	if err != nil {
		return store.Document{}, err
	}
	s.contentChanged(context.WithValue(ctx, publishingKey{}, doc.ID), a, doc.ID)
	s.linkMentions(ctx, p, doc.ID, text)
	if created {
		s.event(ctx, a, "document.update", doc.ID, map[string]int{"version": saved.Version})
	}
	s.indexDocument(ctx, saved)
	return saved, nil
}

// WhiteboardOf reads the board of a whiteboard document.
func WhiteboardOf(doc store.Document) (*Whiteboard, error) {
	if doc.Type != store.DocWhiteboard {
		return nil, invalid("validation.not_whiteboard", nil, "%s is not a whiteboard", doc.Slug)
	}
	wb := &Whiteboard{}
	if len(doc.Content) > 0 {
		if err := json.Unmarshal(doc.Content, wb); err != nil {
			return nil, err
		}
	}
	if wb.Elements == nil {
		wb.Elements = []WBElement{}
	}
	return wb, nil
}

// ---- embed previews -------------------------------------------------------

var (
	metaRe  = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)
	attrRe  = regexp.MustCompile(`(?is)(property|name|content)\s*=\s*("([^"]*)"|'([^']*)')`)
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// EmbedKind guesses the kind of an embed from its address.
func EmbedKind(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "link"
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch {
	case host == "youtube.com" || host == "youtu.be" || host == "m.youtube.com" || host == "vimeo.com" || host == "loom.com" || strings.HasSuffix(host, ".loom.com"):
		return "video"
	case (host == "github.com" || strings.Contains(host, "gitea") || strings.HasPrefix(host, "git.") || strings.Contains(host, "codeberg")) &&
		regexp.MustCompile(`/(pull|pulls|issues|commit|releases)/`).MatchString(u.Path):
		return "code"
	}
	return "link"
}

// EmbedPreview fetches title, description and site of an address for an
// embed card (same network rules as request blocks: no private addresses
// when hosted, size and time limits). It never fails hard: without an
// answer the card shows the address.
func (s *Service) EmbedPreview(ctx context.Context, a Actor, ref, raw string) (WBPreview, string, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return WBPreview{}, "", err
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return WBPreview{}, "", invalid("validation.link_url", nil, "url must be an http(s) URL")
	}
	kind := EmbedKind(raw)
	pv := WBPreview{Site: strings.TrimPrefix(u.Hostname(), "www."), Title: raw}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return pv, kind, nil
	}
	req.Header.Set("User-Agent", "Phoedox link preview")
	req.Header.Set("Accept", "text/html")
	res, err := s.RequestPolicy.Client().Do(req)
	if err != nil {
		return pv, kind, nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return pv, kind, nil
	}
	pv.Frameable = frameable(res.Header) && s.embedPolicy(ctx, p).allows(u.Hostname())
	body, _ := io.ReadAll(io.LimitReader(res.Body, 512<<10))
	meta := map[string]string{}
	for _, tag := range metaRe.FindAllString(string(body), 200) {
		var key, val string
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			v := m[3] + m[4]
			switch strings.ToLower(m[1]) {
			case "property", "name":
				key = strings.ToLower(v)
			case "content":
				val = v
			}
		}
		if key != "" && meta[key] == "" {
			meta[key] = strings.TrimSpace(html.UnescapeString(val))
		}
	}
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := meta[k]; v != "" {
				return v
			}
		}
		return ""
	}
	if t := first("og:title", "twitter:title"); t != "" {
		pv.Title = t
	} else if m := titleRe.FindStringSubmatch(string(body)); m != nil {
		pv.Title = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	pv.Description = first("og:description", "twitter:description", "description")
	if site := first("og:site_name"); site != "" {
		pv.Site = site
	}
	pv.Title = clip(pv.Title, 500)
	pv.Description = clip(pv.Description, 2000)
	pv.Site = clip(pv.Site, 200)
	return pv, kind, nil
}

// frameable reports whether a page may be shown in a frame on another site.
func frameable(h http.Header) bool {
	if xfo := strings.ToLower(strings.TrimSpace(h.Get("X-Frame-Options"))); xfo != "" {
		return false
	}
	for _, csp := range h.Values("Content-Security-Policy") {
		for _, dir := range strings.Split(csp, ";") {
			f := strings.Fields(strings.ToLower(dir))
			if len(f) == 0 || f[0] != "frame-ancestors" {
				continue
			}
			return slices.Contains(f[1:], "*")
		}
	}
	return true
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8Valid(s) {
		s = s[:len(s)-1]
	}
	return s
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "�") == s }

// errorsOnly drops warnings.
func errorsOnly(ps []blocks.Problem) []blocks.Problem {
	var out []blocks.Problem
	for _, p := range ps {
		if p.Severity != "warning" {
			out = append(out, p)
		}
	}
	return out
}
