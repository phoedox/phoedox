package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestPrototypeValidation(t *testing.T) {
	base := func() *Whiteboard {
		return &Whiteboard{
			Prototypes: []WBPrototype{{ID: "p1", Name: "Shop", Vars: []WBVar{{Name: "count", Type: "number", Value: json.RawMessage("1")}},
				Data: []WBData{{Name: "items", Source: "sample", Rows: []map[string]any{{"title": "Beans", "price": 12.9}}}}}},
			Elements: []WBElement{
				{ID: "s1", Type: "frame", X: 0, Y: 0, W: 390, H: 844, Screen: &WBScreen{Proto: "p1", Device: "phone"}},
				{ID: "s2", Type: "frame", X: 500, Y: 0, W: 390, H: 300, Screen: &WBScreen{Proto: "p1", Overlay: "sheet"}},
				{ID: "b1", Type: "ui", X: 16, Y: 16, W: 200, H: 48, Frame: "s1", Text: "Pay {{count}}", UI: &WBUI{Comp: "button"},
					Actions: []WBAction{{On: "click", Do: "overlay", Target: "s2"}, {On: "click", Do: "inc", Target: "count"}}},
			},
		}
	}
	if _, text, err := whiteboardContent(base()); err != nil || !strings.Contains(text, "Shop") || !strings.Contains(text, "Pay {{count}}") {
		t.Fatalf("valid board: %v %q", err, text)
	}
	cases := map[string]func(wb *Whiteboard){
		"unknown component":    func(wb *Whiteboard) { wb.Elements[2].UI.Comp = "spaceship" },
		"ui without component": func(wb *Whiteboard) { wb.Elements[2].UI = nil },
		"goto a non-screen":    func(wb *Whiteboard) { wb.Elements[2].Actions[0].Target = "b1" },
		"bad trigger":          func(wb *Whiteboard) { wb.Elements[2].Actions[0].On = "explode" },
		"script link": func(wb *Whiteboard) {
			wb.Elements[2].Actions[0] = WBAction{On: "click", Do: "link", Value: "javascript:alert(1)"}
		},
		"screen of nothing": func(wb *Whiteboard) { wb.Elements[0].Screen.Proto = "nope" },
		"screen on a shape": func(wb *Whiteboard) { wb.Elements[2].Screen = &WBScreen{Proto: "p1"} },
		"variable type":     func(wb *Whiteboard) { wb.Prototypes[0].Vars[0].Value = json.RawMessage(`"one"`) },
		"nested data":       func(wb *Whiteboard) { wb.Prototypes[0].Data[0].Rows[0]["x"] = map[string]any{"y": 1} },
		"live without ref":  func(wb *Whiteboard) { wb.Prototypes[0].Data[0].Source = "query" },
		"bad look":          func(wb *Whiteboard) { wb.Prototypes[0].Look = "neon" },
		"bad show":          func(wb *Whiteboard) { wb.Elements[2].UI.Show = "a b" },
	}
	for name, change := range cases {
		wb := base()
		change(wb)
		if _, _, err := whiteboardContent(wb); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A start screen that is gone is forgotten, not an error.
	wb := base()
	wb.Prototypes[0].Start = "gone"
	if _, _, err := whiteboardContent(wb); err != nil || wb.Prototypes[0].Start != "" {
		t.Fatalf("start: %v %q", err, wb.Prototypes[0].Start)
	}
}

func TestDrawScreensExportCommentAndApp(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Shop"})
	in := ScreensInput{Prototype: "Checkout", Vars: []WBVar{{Name: "count", Type: "number", Value: json.RawMessage("1")}},
		Data: []WBData{{Name: "cards", Source: "tasks", Ref: "nope", Rows: []map[string]any{{"title": "Sample"}}}},
		Screens: []ScreenInput{
			{Name: "Cart", Start: true, Items: []ScreenElement{
				{Type: "text", Text: "Your cart", FontSize: 28},
				{Comp: "button", Text: "Pay", Actions: []WBAction{{Do: "overlay", Target: "paid"}}},
				{Comp: "navbar", WBUI: WBUI{Options: []string{"Home", "Cart"}, Fields: []string{"house", "shopping-cart"}}},
			}},
			{Name: "Paid", Overlay: "sheet", Items: []ScreenElement{{Comp: "button", Text: "Close", Actions: []WBAction{{Do: "close"}}}}},
		}}
	res, err := s.DrawScreens(ctx, owner, p.Slug, "", "", "", in)
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Document
	wb, _ := WhiteboardOf(*doc)
	if len(wb.Prototypes) != 1 || wb.Prototypes[0].Start != res.Screens["Cart"] || len(wb.Elements) != 6 {
		t.Fatalf("drawn: %+v %d", wb.Prototypes, len(wb.Elements))
	}
	var pay, nav, cart WBElement
	for _, e := range wb.Elements {
		switch {
		case e.Text == "Pay":
			pay = e
		case e.UI != nil && e.UI.Comp == "navbar":
			nav = e
		case e.ID == res.Screens["Cart"]:
			cart = e
		}
	}
	if pay.Actions[0].Target != res.Screens["Paid"] || pay.Actions[0].On != "click" || pay.Frame != cart.ID {
		t.Fatalf("pay: %+v", pay)
	}
	if nav.Y+nav.H != cart.Y+cart.H || nav.W != cart.W {
		t.Fatalf("navbar not at the bottom: %+v of %+v", nav, cart)
	}
	// Redrawing a screen by name replaces its content in place.
	again, err := s.DrawScreens(ctx, owner, p.Slug, doc.Slug, "", "", ScreensInput{Prototype: "checkout", Screens: []ScreenInput{{Name: "Cart", Items: []ScreenElement{{Type: "text", Text: "Empty"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	wb, _ = WhiteboardOf(*again.Document)
	if again.Screens["Cart"] != cart.ID || len(wb.Elements) != 4 || len(wb.Prototypes) != 1 {
		t.Fatalf("redraw: %+v %d", again.Screens, len(wb.Elements))
	}
	view, _ := s.WhiteboardView(ctx, owner, p.Slug, doc.Slug)
	if len(view.Prototypes) != 1 {
		t.Fatalf("view: %+v", view.Prototypes)
	}

	// Data: a live source that cannot be read shows its sample rows.
	data, err := s.PrototypeData(ctx, owner, p.Slug, doc.Slug, "Checkout")
	if err != nil || len(data["cards"]) != 1 || data["cards"][0]["title"] != "Sample" {
		t.Fatalf("data: %v %v", data, err)
	}

	page, name, err := s.PrototypeHTML(ctx, owner, p.Slug, doc.Slug, res.Prototype, "de", MaxExportImages)
	if err != nil || name != "Checkout" {
		t.Fatal(err, name)
	}
	html := string(page)
	if !strings.Contains(html, "globalThis.PhxProto") || !strings.Contains(html, `"proto":"checkout"`) || strings.Count(html, "</script>") != 2 || !strings.Contains(html, `lang="de"`) {
		t.Fatalf("export: %.400s", html)
	}

	// Comments while playing: readers may comment on a screen.
	if err := s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader); err != nil {
		t.Fatal(err)
	}
	c, err := s.CommentWhiteboard(ctx, reader, p.Slug, doc.Slug, PlayComment{Screen: cart.ID, X: 20, Y: 30, Text: "Bigger button?"})
	if err != nil || c.Element != cart.ID || c.X != cart.X+20 {
		t.Fatalf("comment: %+v %v", c, err)
	}
	if _, err := s.CommentWhiteboard(ctx, reader, p.Slug, doc.Slug, PlayComment{Screen: cart.ID, X: 5000, Y: 1, Text: "x"}); err == nil {
		t.Fatal("comment outside the screen accepted")
	}
	if _, err := s.PrototypeToApp(ctx, reader, p.Slug, doc.Slug, res.Prototype, ""); err == nil {
		t.Fatal("reader made an app")
	}
	app, err := s.PrototypeToApp(ctx, owner, p.Slug, doc.Slug, res.Prototype, "en")
	if err != nil || app.Document == nil || app.Document.Type != store.DocApp {
		t.Fatalf("app: %+v %v", app, err)
	}
}
