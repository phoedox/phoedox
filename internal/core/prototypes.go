package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"math/rand/v2"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/web"
)

// Prototypes on whiteboards (part A of the concept "Prototypes and apps"):
// frames become screens of a device, components (type "ui") are drawn with
// Phoedox's tokens or as a sketch, and interactions (trigger → action) link
// screens, open overlays and change a few variables. One player script
// (web/player/proto-player.js) plays them in the editor, in the app, in
// share links and in the exported HTML file.

// WBPrototype groups the screens of one prototype.
type WBPrototype struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Start is the screen shown first (a frame id); default: the first screen.
	Start string `json:"start,omitempty"`
	// Look: "" draws components with the theme's tokens, "sketch" as a wireframe.
	Look string   `json:"look,omitempty"`
	Vars []WBVar  `json:"vars,omitempty"`
	Data []WBData `json:"data,omitempty"`
}

// WBVar is a variable of a prototype; components bind to it.
type WBVar struct {
	Name  string          `json:"name"`
	Type  string          `json:"type"` // bool | text | number | list
	Value json.RawMessage `json:"value,omitempty"`
}

// WBData is a data source for lists, tables, charts and selects: sample
// rows, or read-only live data of the project (a saved query, the cards of
// a board, the checks). Live sources show their sample rows wherever the
// viewer may not read the project (share links, exported files).
type WBData struct {
	Name   string           `json:"name"`
	Source string           `json:"source"` // sample | query | tasks | checks
	Ref    string           `json:"ref,omitempty"`
	Rows   []map[string]any `json:"rows,omitempty"`
}

// WBScreen makes a frame a screen of a prototype.
type WBScreen struct {
	Proto  string `json:"proto"`
	Device string `json:"device,omitempty"` // phone | tablet | desktop | custom
	// Overlay: the screen opens over another one (modal or bottom sheet).
	Overlay string `json:"overlay,omitempty"`
	// View: the visible height when the screen is taller than its device;
	// the player scrolls the screen.
	View float64 `json:"view,omitempty"`
}

// WBUI describes a component. Its label is the element's text.
type WBUI struct {
	Comp        string   `json:"comp"`
	Variant     string   `json:"variant,omitempty"` // buttons: primary | secondary | ghost | danger
	Placeholder string   `json:"placeholder,omitempty"`
	Value       string   `json:"value,omitempty"`
	Options     []string `json:"options,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	// Bind: the variable the component shows and changes.
	Bind string `json:"bind,omitempty"`
	// Data: the data source of lists, tables, charts and selects; Fields the
	// columns shown (lists: title, subtitle).
	Data   string   `json:"data,omitempty"`
	Fields []string `json:"fields,omitempty"`
	Min    *float64 `json:"min,omitempty"`
	Max    *float64 `json:"max,omitempty"`
	// Show: shown only when a condition holds ("var", "!var", "var=value").
	Show string `json:"show,omitempty"`
}

// WBAction is an interaction: when On happens, Do.
type WBAction struct {
	On     string `json:"on"`               // click | change | hover | open | delay
	Do     string `json:"do"`               // goto | back | overlay | close | set | toggle | inc | add | link | toast
	Target string `json:"target,omitempty"` // a screen (goto, overlay) or a variable (set, toggle, inc, add)
	Value  string `json:"value,omitempty"`  // set: the value; inc: the step; add: the item; link: the address; toast: the text
	Delay  int    `json:"delay,omitempty"`  // ms, for "delay"
	Anim   string `json:"anim,omitempty"`   // "" | slide | fade
	// Item limits the action to one option of tabs, navigation bars,
	// radios and selects, or one row of a list (by its title).
	Item string `json:"item,omitempty"`
}

const (
	maxProtos      = 50
	maxProtoVars   = 50
	maxProtoData   = 20
	maxProtoRows   = 500
	maxProtoCols   = 30
	maxProtoAction = 20
	maxProtoLive   = 200
)

var (
	protoComps = map[string]bool{"button": true, "link": true, "input": true, "textarea": true, "checkbox": true, "switch": true, "radio": true,
		"select": true, "slider": true, "tabs": true, "navbar": true, "list": true, "card": true, "table": true, "toast": true, "icon": true,
		"chart": true, "avatar": true, "badge": true, "progress": true}
	protoVarRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,39}$`)
	protoIconRe  = regexp.MustCompile(`^[a-z0-9-]{0,60}$`)
	protoShowRe  = regexp.MustCompile(`^!?[A-Za-z_][A-Za-z0-9_]{0,39}(=.{0,200})?$`)
	protoOn      = map[string]bool{"click": true, "change": true, "hover": true, "open": true, "delay": true}
	protoDo      = map[string]bool{"goto": true, "back": true, "overlay": true, "close": true, "set": true, "toggle": true, "inc": true, "add": true, "link": true, "toast": true}
	protoDevices = map[string]bool{"": true, "phone": true, "tablet": true, "desktop": true, "custom": true}
)

func badProto(what string) error {
	return invalid("validation.prototype", map[string]any{"what": what}, "prototype: invalid %s", what)
}

// checkPrototypeElement checks the prototype parts of one element.
func checkPrototypeElement(wb *Whiteboard, e *WBElement, ids map[string]bool) error {
	bad := func(what string) error { return badProto(fmt.Sprintf("%s (%s)", what, e.ID)) }
	if (e.Type == "ui") != (e.UI != nil) {
		return bad("component")
	}
	if e.Screen != nil && !slices.ContainsFunc(wb.Prototypes, func(p WBPrototype) bool { return p.ID == e.Screen.Proto }) {
		// Its prototype was deleted (perhaps by someone else at the same time).
		e.Screen = nil
	}
	if e.Screen != nil {
		if e.Type != "frame" || !protoDevices[e.Screen.Device] {
			return bad("screen")
		}
		switch e.Screen.Overlay {
		case "", "modal", "sheet":
		default:
			return bad("overlay")
		}
		if e.Screen.View < 0 || e.Screen.View > 6000 || math.IsNaN(e.Screen.View) || (e.Screen.View > 0 && e.Screen.View >= e.H) {
			e.Screen.View = 0
		}
	}
	if u := e.UI; u != nil {
		if !protoComps[u.Comp] || len(u.Variant) > 20 || len(u.Placeholder) > 500 || len(u.Value) > 2000 || len(u.Options) > 100 || !protoIconRe.MatchString(u.Icon) ||
			len(u.Fields) > maxProtoCols || len(u.Show) > 240 || (u.Show != "" && !protoShowRe.MatchString(u.Show)) {
			return bad("component")
		}
		if (u.Bind != "" && !protoVarRe.MatchString(u.Bind)) || (u.Data != "" && !protoVarRe.MatchString(u.Data)) {
			return bad("binding")
		}
		for _, o := range append(slices.Clone(u.Options), u.Fields...) {
			if len(o) > 200 {
				return bad("options")
			}
		}
		for _, f := range []*float64{u.Min, u.Max} {
			if f != nil && (math.IsNaN(*f) || math.IsInf(*f, 0)) {
				return bad("range")
			}
		}
	}
	if len(e.Actions) > maxProtoAction {
		return bad("actions")
	}
	// Interactions with a screen that is gone are dropped.
	e.Actions = slices.DeleteFunc(e.Actions, func(ac WBAction) bool {
		return (ac.Do == "goto" || ac.Do == "overlay") && !slices.ContainsFunc(wb.Elements, func(x WBElement) bool { return x.ID == ac.Target && x.Screen != nil })
	})
	if len(e.Actions) == 0 {
		e.Actions = nil
	}
	for _, ac := range e.Actions {
		if !protoOn[ac.On] || !protoDo[ac.Do] || ac.Delay < 0 || ac.Delay > 600_000 || len(ac.Value) > 2000 || len(ac.Item) > 200 {
			return bad("action")
		}
		switch ac.Anim {
		case "", "slide", "fade":
		default:
			return bad("action")
		}
		switch ac.Do {
		case "set", "toggle", "inc", "add":
			if !protoVarRe.MatchString(ac.Target) {
				return bad("action target")
			}
		case "link":
			u, err := url.Parse(ac.Value)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto") {
				return bad("link")
			}
		}
	}
	return nil
}

func scalar(v any) bool {
	switch v.(type) {
	case nil, string, float64, bool, json.Number:
		return true
	}
	return false
}

// checkPrototypes checks the prototypes of a board; returns their texts.
func checkPrototypes(wb *Whiteboard) ([]string, error) {
	if len(wb.Prototypes) > maxProtos {
		return nil, badProto("count")
	}
	var text []string
	seen := map[string]bool{}
	for i := range wb.Prototypes {
		p := &wb.Prototypes[i]
		p.Name = strings.TrimSpace(p.Name)
		if !planIDRe.MatchString(p.ID) || seen[p.ID] || p.Name == "" || len(p.Name) > 200 || len(p.Vars) > maxProtoVars || len(p.Data) > maxProtoData {
			return nil, badProto("prototype")
		}
		seen[p.ID] = true
		switch p.Look {
		case "", "sketch":
		default:
			return nil, badProto("look")
		}
		if p.Start != "" && !slices.ContainsFunc(wb.Elements, func(e WBElement) bool { return e.ID == p.Start && e.Screen != nil && e.Screen.Proto == p.ID }) {
			p.Start = "" // the start screen is gone: the first screen starts
		}
		names := map[string]bool{}
		for _, v := range p.Vars {
			if !protoVarRe.MatchString(v.Name) || names[v.Name] || len(v.Value) > 20_000 {
				return nil, badProto("variable " + v.Name)
			}
			names[v.Name] = true
			var val any
			if len(v.Value) > 0 {
				if err := json.Unmarshal(v.Value, &val); err != nil {
					return nil, badProto("variable " + v.Name)
				}
			}
			ok := val == nil
			switch x := val.(type) {
			case bool:
				ok = v.Type == "bool"
			case string:
				ok = v.Type == "text"
			case float64:
				ok = v.Type == "number"
			case []any:
				ok = v.Type == "list" && len(x) <= maxProtoRows && !slices.ContainsFunc(x, func(it any) bool { return !scalar(it) })
			}
			if !ok || (v.Type != "bool" && v.Type != "text" && v.Type != "number" && v.Type != "list") {
				return nil, badProto("variable " + v.Name)
			}
		}
		dnames := map[string]bool{}
		for _, d := range p.Data {
			if !protoVarRe.MatchString(d.Name) || dnames[d.Name] || len(d.Ref) > 200 || len(d.Rows) > maxProtoRows {
				return nil, badProto("data " + d.Name)
			}
			dnames[d.Name] = true
			switch d.Source {
			case "sample", "checks":
			case "query", "tasks":
				if d.Ref == "" {
					return nil, badProto("data " + d.Name)
				}
			default:
				return nil, badProto("data " + d.Name)
			}
			for _, r := range d.Rows {
				if len(r) > maxProtoCols {
					return nil, badProto("data " + d.Name)
				}
				for k, v := range r {
					if len(k) > 100 || !scalar(v) {
						return nil, badProto("data " + d.Name)
					}
					if s, ok := v.(string); ok && len(s) > 2000 {
						return nil, badProto("data " + d.Name)
					}
				}
			}
		}
		text = append(text, p.Name)
	}
	return text, nil
}

func (wb *Whiteboard) prototype(id string) (*WBPrototype, bool) {
	for i := range wb.Prototypes {
		if wb.Prototypes[i].ID == id || strings.EqualFold(wb.Prototypes[i].Name, id) {
			return &wb.Prototypes[i], true
		}
	}
	return nil, false
}

// prototypeBoard keeps what a prototype needs: its screens, the elements
// on them and the prototype itself (for players outside the board).
func prototypeBoard(wb *Whiteboard, p *WBPrototype) *Whiteboard {
	out := &Whiteboard{Version: 1, Elements: []WBElement{}, Prototypes: []WBPrototype{*p}}
	var screens []WBElement
	for _, e := range wb.Elements {
		if e.Screen != nil && e.Screen.Proto == p.ID {
			screens = append(screens, e)
		}
	}
	parent := map[string]string{}
	for _, e := range wb.Elements {
		parent[e.ID] = e.Frame
	}
	in := func(e WBElement) bool {
		for f, n := e.Frame, 0; f != "" && n < 20; f, n = parent[f], n+1 {
			if slices.ContainsFunc(screens, func(s WBElement) bool { return s.ID == f }) {
				return true
			}
		}
		minX, minY, maxX, maxY := e.X, e.Y, e.X+e.W, e.Y+e.H
		if len(e.Points) > 0 {
			minX, minY, maxX, maxY = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
			for _, pt := range e.Points {
				minX, minY, maxX, maxY = math.Min(minX, pt[0]), math.Min(minY, pt[1]), math.Max(maxX, pt[0]), math.Max(maxY, pt[1])
			}
		}
		cx, cy := (minX+maxX)/2, (minY+maxY)/2
		return slices.ContainsFunc(screens, func(s WBElement) bool { return cx >= s.X && cx <= s.X+s.W && cy >= s.Y && cy <= s.Y+s.H })
	}
	for _, e := range wb.Elements {
		if (e.Screen != nil && e.Screen.Proto == p.ID) || (e.Screen == nil && in(e)) {
			out.Elements = append(out.Elements, e)
		}
	}
	return out
}

func flatRow(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if scalar(v) {
			out[k] = v
		} else if b, err := json.Marshal(v); err == nil {
			out[k] = string(b)
		}
	}
	return out
}

// PrototypeData reads the data sources of a prototype with the viewer's
// rights: live sources from the project, sample rows where the live
// source cannot be read.
func (s *Service) PrototypeData(ctx context.Context, a Actor, ref, docRef, protoID string) (map[string][]map[string]any, error) {
	p, _, wb, err := s.whiteboardDoc(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, err
	}
	pr, ok := wb.prototype(protoID)
	if !ok {
		return nil, ErrNotFound
	}
	return s.prototypeData(ctx, a, p, pr), nil
}

func (s *Service) prototypeData(ctx context.Context, a Actor, p store.Project, pr *WBPrototype) map[string][]map[string]any {
	out := map[string][]map[string]any{}
	for _, d := range pr.Data {
		rows := d.Rows
		switch d.Source {
		case "query":
			if res, err := s.RunSavedQuery(ctx, a, p.Slug, d.Ref); err == nil {
				rows = []map[string]any{}
				for _, r := range res.Rows {
					m := flatRow(r.Attrs)
					m["name"], m["slug"], m["kind"] = r.Name, r.Slug, r.Kind
					rows = append(rows, m)
				}
			}
		case "tasks":
			if list, err := s.Tasks(ctx, a, p.Slug, d.Ref, "", "", false); err == nil {
				rows = []map[string]any{}
				for _, t := range list {
					who := t.AssigneeName
					if who == "" {
						who = t.AssigneeText
					}
					rows = append(rows, map[string]any{"title": t.Title, "column": t.Column, "assignee": who, "due": t.Due,
						"priority": t.Priority, "labels": strings.Join(t.Labels, ", "), "done": !t.DoneAt.IsZero()})
				}
			}
		case "checks":
			if list, err := s.Checks(ctx, a, p.Slug, ""); err == nil {
				rows = []map[string]any{}
				for _, c := range list {
					rows = append(rows, map[string]any{"name": c.Name, "status": c.LastStatus, "entity": c.EntityName, "page": c.DocumentTitle})
				}
			}
		}
		if len(rows) > maxProtoLive && d.Source != "sample" {
			rows = rows[:maxProtoLive]
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		out[d.Name] = rows
	}
	return out
}

// sampleData is the data of a prototype without reading the project.
func sampleData(pr *WBPrototype) map[string][]map[string]any {
	out := map[string][]map[string]any{}
	for _, d := range pr.Data {
		out[d.Name] = d.Rows
		if out[d.Name] == nil {
			out[d.Name] = []map[string]any{}
		}
	}
	return out
}

// PlayerModel is what the player needs to play a prototype.
type PlayerModel struct {
	Board *Whiteboard                 `json:"board"`
	Proto string                      `json:"proto"`
	Data  map[string][]map[string]any `json:"data"`
	// Files maps image file ids to addresses (data: URLs in exports).
	Files map[string]string `json:"files,omitempty"`
	Lang  string            `json:"lang,omitempty"`
}

// MaxExportImages limits the images put into an exported prototype.
const MaxExportImages = 8 << 20

// PrototypeHTML builds one HTML file that plays a prototype on its own:
// the player, the screens, the data as of now (live data with the
// viewer's rights) and the images inline.
func (s *Service) PrototypeHTML(ctx context.Context, a Actor, ref, docRef, protoID, lang string, imageBytes int) ([]byte, string, error) {
	p, _, wb, err := s.whiteboardDoc(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, "", err
	}
	pr, ok := wb.prototype(protoID)
	if !ok {
		return nil, "", ErrNotFound
	}
	m := PlayerModel{Board: prototypeBoard(wb, pr), Proto: pr.ID, Data: s.prototypeData(ctx, a, p, pr), Files: map[string]string{}, Lang: lang}
	budget := imageBytes
	for _, e := range m.Board.Elements {
		if e.File == "" || m.Files[e.File] != "" {
			continue
		}
		fc, err := s.OpenFile(ctx, a, e.File, 0, false)
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(fc.Reader, int64(budget)+1))
		fc.Reader.Close()
		if len(b) > budget || !strings.HasPrefix(fc.Mime, "image/") {
			continue
		}
		budget -= len(b)
		m.Files[e.File] = "data:" + fc.Mime + ";base64," + base64.StdEncoding.EncodeToString(b)
	}
	page, err := PlayerPage(pr.Name, m, "")
	return page, pr.Name, err
}

// appHooks lets the Phoedox app (a WebView with the PhoedoxBridge channel)
// save comments and open links; elsewhere the file plays on its own.
const appHooks = `function appHooks() {
  const b = window.PhoedoxBridge;
  if (!b || !b.postMessage) return {};
  let n = 0; const wait = {};
  window.__phxReply = (id, ok, data) => { const w = wait[id]; delete wait[id]; if (w) (ok ? w[0] : w[1])(data); };
  return {
    onComment: (c) => new Promise((res, rej) => { const id = ++n; wait[id] = [res, rej]; b.postMessage(JSON.stringify({ op: "comment", id: id, comment: c })); }),
    onLink: (url) => b.postMessage(JSON.stringify({ op: "link", url: url })),
  };
}`

// PlayerPage is a page that plays a prototype; nonce marks its scripts
// for a CSP (empty: a file without CSP).
func PlayerPage(title string, m PlayerModel, nonce string) ([]byte, error) {
	model, err := json.Marshal(m) // escapes <, > and & for the script
	if err != nil {
		return nil, err
	}
	n := ""
	if nonce != "" {
		n = ` nonce="` + html.EscapeString(nonce) + `"`
	}
	lang := m.Lang
	if lang == "" {
		lang = "en"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "<!doctype html>\n<html lang=\"%s\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">"+
		"<title>%s</title><style%s>html,body{margin:0;height:100%%}#phx-proto{position:fixed;inset:0}</style></head>"+
		"<body><div id=\"phx-proto\"></div><script%s>\n%s\n</script><script%s>%s\nPhxProto.mount(document.getElementById('phx-proto'), %s, appHooks());</script></body></html>\n",
		html.EscapeString(lang), html.EscapeString(title), n, n, strings.ReplaceAll(web.ProtoPlayer, "</script", "<\\/script"), n, appHooks, model)
	return b.Bytes(), nil
}

// ---- comments while playing -----------------------------------------------

// PlayComment is a comment pinned to a spot of a screen while playing.
type PlayComment struct {
	Screen string  `json:"screen"`
	X      float64 `json:"x"` // relative to the screen
	Y      float64 `json:"y"`
	Text   string  `json:"text"`
}

// CommentWhiteboard adds a comment pin to a board; people who may read the
// board may comment (as on pages). Open editors reload the board.
func (s *Service) CommentWhiteboard(ctx context.Context, a Actor, ref, docRef string, in PlayComment) (WBComment, error) {
	p, doc, wb, err := s.whiteboardDoc(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return WBComment{}, err
	}
	if s.readOnly(a, p) {
		return WBComment{}, ErrForbidden
	}
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" || len(in.Text) > maxWBText {
		return WBComment{}, invalid("validation.comment_text", nil, "the comment needs a text")
	}
	at := slices.IndexFunc(wb.Elements, func(e WBElement) bool { return e.ID == in.Screen })
	if at < 0 || in.X < 0 || in.Y < 0 || in.X > wb.Elements[at].W || in.Y > wb.Elements[at].H {
		return WBComment{}, badProto("comment")
	}
	sc := wb.Elements[at]
	name := a.User.Name
	if a.Via == "mcp" {
		name = "AI"
	}
	c := WBComment{ID: fmt.Sprintf("c%x%03x", time.Now().UnixMilli(), rand.IntN(4096)), X: sc.X + in.X, Y: sc.Y + in.Y, Element: sc.ID,
		Author: a.String(), Name: name, Text: in.Text, At: time.Now().UTC().Format(time.RFC3339)}
	wb.Comments = append(wb.Comments, c)
	content, text, err := whiteboardContent(wb)
	if err != nil {
		return WBComment{}, err
	}
	doc.Content = content
	saved, created, err := s.DB.SaveLive(ctx, doc, a.String(), liveWindow)
	if err != nil {
		return WBComment{}, err
	}
	s.contentChanged(ctx, a, doc.ID)
	s.linkMentions(ctx, p, doc.ID, text)
	if created {
		s.event(ctx, a, "document.update", doc.ID, map[string]int{"version": saved.Version})
	}
	s.event(ctx, a, "whiteboard.comment", doc.ID, map[string]string{"comment": c.ID})
	s.indexDocument(ctx, saved)
	return c, nil
}

// ---- make it an app ---------------------------------------------------------

// PrototypeToApp creates an app document from a prototype: the exported
// player file as its index.html, ready for the AI or people to turn into a
// real app.
func (s *Service) PrototypeToApp(ctx context.Context, a Actor, ref, docRef, protoID, lang string) (DocResult, error) {
	p, doc, _, err := s.whiteboardDoc(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	// Apps are at most maxAppBytes: images only while they fit.
	page, name, err := s.PrototypeHTML(ctx, a, ref, docRef, protoID, lang, maxAppBytes/2)
	if err != nil {
		return DocResult{}, err
	}
	return s.CreateDocument(ctx, a, p.Slug, DocumentInput{Type: store.DocApp, Title: name, Slug: s.freeSlug(ctx, p, name+" app", map[string]bool{}), Category: doc.Category,
		App: &App{Files: map[string]string{"index.html": string(page)}}})
}
