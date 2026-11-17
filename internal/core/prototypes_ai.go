package core

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// ScreensInput is what draw_screens lays out: a prototype (new or
// existing, by name or id) with screens of components. Screens with the
// name of an existing screen of the prototype are redrawn in place.
type ScreensInput struct {
	Prototype string        `json:"prototype"`
	Device    string        `json:"device,omitempty"` // phone (default) | tablet | desktop
	Look      string        `json:"look,omitempty"`   // "" | sketch
	Vars      []WBVar       `json:"vars,omitempty"`   // added or replaced by name
	Data      []WBData      `json:"data,omitempty"`   // added or replaced by name
	Screens   []ScreenInput `json:"screens"`
}

// ScreenInput is one screen.
type ScreenInput struct {
	Name    string          `json:"name"`
	Start   bool            `json:"start,omitempty"`
	Overlay string          `json:"overlay,omitempty"` // "" | modal | sheet
	W       float64         `json:"w,omitempty"`
	H       float64         `json:"h,omitempty"`
	Fill    string          `json:"fill,omitempty"`
	Actions []WBAction      `json:"actions,omitempty"` // on open / delay; targets by screen name
	Items   []ScreenElement `json:"elements"`
}

// ScreenElement is a component (comp) or a plain element (type: text,
// rect, ellipse, sticky, line) on a screen. Without a position, elements
// stack from the top; navigation bars go to the bottom.
type ScreenElement struct {
	Comp     string     `json:"comp,omitempty"`
	Type     string     `json:"type,omitempty"`
	Text     string     `json:"text,omitempty"`
	X        *float64   `json:"x,omitempty"`
	Y        *float64   `json:"y,omitempty"`
	W        float64    `json:"w,omitempty"`
	H        float64    `json:"h,omitempty"`
	FontSize float64    `json:"fontSize,omitempty"`
	Align    string     `json:"align,omitempty"`
	Stroke   string     `json:"stroke,omitempty"`
	Fill     string     `json:"fill,omitempty"`
	Actions  []WBAction `json:"actions,omitempty"`
	WBUI
}

// ScreensResult tells where the screens are.
type ScreensResult struct {
	DocResult
	Prototype string            `json:"prototype"`
	Screens   map[string]string `json:"screens"` // name → frame id
}

var deviceSize = map[string][2]float64{"phone": {390, 844}, "tablet": {820, 1180}, "desktop": {1440, 900}}

// compHeight is the default height of a component in a stack.
func compHeight(it ScreenElement) float64 {
	switch it.Comp {
	case "button", "input", "select", "toast":
		return 48
	case "textarea":
		return 110
	case "checkbox", "badge", "progress", "link":
		return 32
	case "switch":
		return 36
	case "radio":
		return math.Max(32, float64(len(it.Options))*32)
	case "slider":
		return 52
	case "tabs":
		return 40
	case "navbar":
		return 64
	case "list":
		return math.Min(400, math.Max(112, float64(max(len(it.Options), 5))*56))
	case "card":
		return 120
	case "table", "chart":
		return 200
	case "icon":
		return 32
	case "avatar":
		return 56
	}
	switch it.Type {
	case "text":
		size := it.FontSize
		if size == 0 {
			size = 20
		}
		lines := float64(strings.Count(it.Text, "\n") + 1)
		return math.Ceil(size*1.35*lines) + 4
	case "rect", "ellipse", "sticky":
		return 100
	}
	return 48
}

// DrawScreens lays out the screens of a prototype on a whiteboard (a new
// one when docRef is empty).
func (s *Service) DrawScreens(ctx context.Context, a Actor, ref, docRef, title, category string, in ScreensInput) (ScreensResult, error) {
	in.Prototype = strings.TrimSpace(in.Prototype)
	if in.Prototype == "" || len(in.Screens) == 0 || len(in.Screens) > 40 {
		return ScreensResult{}, invalid("validation.prototype", map[string]any{"what": "a name and 1–40 screens"}, "prototype: needs a name and 1–40 screens")
	}
	if in.Device == "" {
		in.Device = "phone"
	}
	if _, ok := deviceSize[in.Device]; !ok {
		return ScreensResult{}, badProto("device")
	}
	wb := &Whiteboard{Elements: []WBElement{}}
	var p store.Project
	var doc store.Document
	var err error
	if docRef != "" {
		if p, doc, wb, err = s.whiteboardDoc(ctx, a, ref, docRef, store.RoleEditor); err != nil {
			return ScreensResult{}, err
		}
	}
	pr, ok := wb.prototype(in.Prototype)
	if !ok {
		wb.Prototypes = append(wb.Prototypes, WBPrototype{ID: freeProtoID(wb, in.Prototype), Name: in.Prototype})
		pr = &wb.Prototypes[len(wb.Prototypes)-1]
	}
	if in.Look != "" {
		pr.Look = in.Look
	}
	for _, v := range in.Vars {
		if i := slices.IndexFunc(pr.Vars, func(x WBVar) bool { return x.Name == v.Name }); i >= 0 {
			pr.Vars[i] = v
		} else {
			pr.Vars = append(pr.Vars, v)
		}
	}
	for _, d := range in.Data {
		if i := slices.IndexFunc(pr.Data, func(x WBData) bool { return x.Name == d.Name }); i >= 0 {
			pr.Data[i] = d
		} else {
			pr.Data = append(pr.Data, d)
		}
	}
	protoID := pr.ID
	order := nextOrder(wb)

	// Where new screens go: right of everything on the board.
	right, top := 0.0, 0.0
	if len(wb.Elements) > 0 {
		right, top = math.Inf(-1), math.Inf(1)
		for _, e := range wb.Elements {
			_, y0, x1, _ := elementBox(e)
			right, top = math.Max(right, x1), math.Min(top, y0)
		}
		right += 200
	}
	screens := map[string]string{}
	existing := map[string]WBElement{}
	for _, e := range wb.Elements {
		if e.Screen != nil && e.Screen.Proto == protoID {
			existing[strings.ToLower(e.Text)] = e
			screens[e.Text] = e.ID
		}
	}
	type placed struct {
		in    ScreenInput
		frame WBElement
	}
	var todo []placed
	x := right
	for _, sc := range in.Screens {
		sc.Name = strings.TrimSpace(sc.Name)
		if sc.Name == "" || len(sc.Items) > 200 {
			return ScreensResult{}, badProto("screen " + sc.Name)
		}
		w, h := deviceSize[in.Device][0], deviceSize[in.Device][1]
		switch sc.Overlay {
		case "modal":
			w, h = math.Min(w-40, 340), 280
		case "sheet":
			h = 360
		}
		if sc.W > 0 {
			w = math.Min(sc.W, 4000)
		}
		if sc.H > 0 {
			h = math.Min(sc.H, 6000)
		}
		var f WBElement
		if old, ok := existing[strings.ToLower(sc.Name)]; ok {
			// Redraw in place: drop what was on the screen.
			f = old
			wb.Elements = slices.DeleteFunc(wb.Elements, func(e WBElement) bool { return e.Frame == old.ID || e.ID == old.ID })
			f.W, f.H = w, h
		} else {
			f = WBElement{ID: freeID(wb, "screen-"+sc.Name), Type: "frame", X: x, Y: top, W: w, H: h}
			x += w + 120
		}
		f.Text, f.Fill, f.Order = sc.Name, sc.Fill, order()
		f.Screen = &WBScreen{Proto: protoID, Device: in.Device, Overlay: sc.Overlay}
		if sc.W > 0 || sc.H > 0 {
			f.Screen.Device = "custom"
		}
		wb.Elements = append(wb.Elements, f)
		screens[sc.Name] = f.ID
		todo = append(todo, placed{in: sc, frame: f})
		if sc.Start {
			pr.Start = f.ID
		}
	}
	// Targets may name screens; give them their ids.
	target := func(acts []WBAction) []WBAction {
		out := slices.Clone(acts)
		for i := range out {
			if out[i].Do == "goto" || out[i].Do == "overlay" {
				if id, ok := screens[out[i].Target]; ok {
					out[i].Target = id
				} else {
					for name, id := range screens {
						if strings.EqualFold(name, out[i].Target) {
							out[i].Target = id
						}
					}
				}
			}
			if out[i].On == "" {
				out[i].On = "click"
			}
		}
		return out
	}
	for _, t := range todo {
		f := t.frame
		fid := f.ID
		cy := 24.0
		var add []WBElement
		for i, it := range t.in.Items {
			e := WBElement{ID: freeID(&Whiteboard{Elements: append(slices.Clone(wb.Elements), add...)}, fmt.Sprintf("%s-%d", fid, i+1)), Frame: fid,
				Text: it.Text, FontSize: it.FontSize, Align: it.Align, Stroke: it.Stroke, Fill: it.Fill, Actions: target(it.Actions)}
			if it.Comp != "" {
				e.Type = "ui"
				u := it.WBUI
				u.Comp = it.Comp
				e.UI = &u
			} else {
				e.Type = it.Type
				if e.Type == "" {
					e.Type = "text"
				}
				if e.Type == "rect" || e.Type == "ellipse" {
					e.Smooth = true
				}
			}
			hgt := it.H
			if hgt == 0 {
				hgt = compHeight(it)
			}
			wdt := it.W
			if wdt == 0 {
				wdt = f.W - 32
				switch it.Comp {
				case "icon":
					wdt = 32
				case "avatar":
					wdt = 56
				case "badge":
					wdt = 96
				case "navbar":
					wdt = f.W
				}
			}
			ex, ey := 16.0, cy
			if it.Comp == "navbar" && it.Y == nil {
				ex, ey = 0, f.H-hgt
			} else if it.Y == nil {
				cy += hgt + 12
			}
			if it.X != nil {
				ex = *it.X
			}
			if it.Y != nil {
				ey = *it.Y
			}
			e.X, e.Y, e.W, e.H = f.X+ex, f.Y+ey, wdt, hgt
			if e.Type == "line" {
				e.Points = [][2]float64{{e.X, e.Y + hgt/2}, {e.X + wdt, e.Y + hgt/2}}
				e.X, e.Y, e.W, e.H = 0, 0, 0, 0
			}
			e.Order = order()
			add = append(add, e)
		}
		wb.Elements = append(wb.Elements, add...)
		if i := slices.IndexFunc(wb.Elements, func(e WBElement) bool { return e.ID == fid }); i >= 0 {
			wb.Elements[i].Actions = target(t.in.Actions)
			for j := range wb.Elements[i].Actions {
				if wb.Elements[i].Actions[j].On == "click" {
					wb.Elements[i].Actions[j].On = "open"
				}
			}
		}
	}
	res := ScreensResult{Prototype: protoID, Screens: screens}
	if docRef == "" {
		if strings.TrimSpace(title) == "" {
			title = in.Prototype
		}
		res.DocResult, err = s.CreateDocument(ctx, a, ref, DocumentInput{Type: store.DocWhiteboard, Title: title, Category: category, Whiteboard: wb})
		return res, err
	}
	res.DocResult, err = s.UpdateDocument(ctx, a, p.Slug, doc.ID, DocumentPatch{Whiteboard: wb})
	return res, err
}

func elementBox(e WBElement) (x0, y0, x1, y1 float64) {
	if len(e.Points) == 0 {
		return e.X, e.Y, e.X + e.W, e.Y + e.H
	}
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range e.Points {
		x0, y0, x1, y1 = math.Min(x0, p[0]), math.Min(y0, p[1]), math.Max(x1, p[0]), math.Max(y1, p[1])
	}
	return
}

func freeProtoID(wb *Whiteboard, name string) string {
	base := strings.Trim(idClean.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(base) > 30 {
		base = base[:30]
	}
	if base == "" {
		base = "proto"
	}
	id := base
	for n := 2; slices.ContainsFunc(wb.Prototypes, func(p WBPrototype) bool { return p.ID == id }); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}
