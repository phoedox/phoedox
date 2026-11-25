package core

import (
	"context"
	"strings"
	"testing"
)

func TestScreenLayoutContainers(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	items := []ScreenElement{
		{ScreenBase: ScreenBase{Type: "text", Text: "Library", Style: "title"}},
		{ScreenBase: ScreenBase{Layout: "row"}, Items: []ScreenChild{
			{ScreenBase: ScreenBase{Comp: "input", Placeholder: "Book id"}},
			{ScreenBase: ScreenBase{Comp: "button", Text: "Add", W: 96}},
		}},
		{ScreenBase: ScreenBase{Layout: "grid", Cols: 2}, Items: []ScreenChild{
			{ScreenBase: ScreenBase{Layout: "column", Style: "card"}, Items: []ScreenGrandchild{
				{ScreenBase: ScreenBase{Type: "text", Text: "Maths 7", Style: "heading"}},
				{ScreenBase: ScreenBase{Type: "text", Text: "318 pages", Style: "caption"}},
			}},
			{ScreenBase: ScreenBase{Layout: "column", Style: "card"}, Items: []ScreenGrandchild{
				{ScreenBase: ScreenBase{Type: "text", Text: "German 7", Style: "heading"}},
			}},
		}},
	}
	out, bottom := screenLayout(items, 390)
	if len(out) != 8 {
		t.Fatalf("items: %d %+v", len(out), out)
	}
	in, btn := out[1], out[2]
	if btn.w != 96 || in.w != 390-32-12-96 || btn.x != in.x+in.w+12 || in.y != btn.y {
		t.Fatalf("row: %+v %+v", in, btn)
	}
	c1, c2 := out[3], out[6]
	if !c1.bg || !c2.bg || c1.Comp != "card" || c1.w != c2.w || c2.x != c1.x+c1.w+12 || c1.h != c2.h {
		t.Fatalf("grid cards: %+v %+v", c1, c2)
	}
	if out[4].x != c1.x+16 || out[4].y != c1.y+16 {
		t.Fatalf("card padding: %+v", out[4])
	}
	if bottom != c1.y+c1.h {
		t.Fatalf("bottom %v, card ends %v", bottom, c1.y+c1.h)
	}
	if ws := layoutWarnings("A", out, 390, 844); len(ws) != 0 {
		t.Fatalf("warnings: %v", ws)
	}
	// Elements placed by hand that cover each other or leave the screen.
	ws := layoutWarnings("A", append(out,
		laidOut{ScreenBase: ScreenBase{Type: "text", Text: "Login", X: f(16)}, x: 16, y: 24, w: 200, h: 30},
		laidOut{ScreenBase: ScreenBase{Comp: "table"}, x: 0, y: 700, w: 390, h: 300}), 390, 844)
	if len(ws) != 2 || !strings.Contains(ws[0], "outside") || !strings.Contains(ws[1], "overlaps") {
		t.Fatalf("warnings: %v", ws)
	}
}

func TestDrawScreensGrowsAndStyles(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Books"})
	var rows []ScreenElement
	rows = append(rows, ScreenElement{ScreenBase: ScreenBase{Type: "text", Text: "Queue", Style: "title"}})
	for i := 0; i < 14; i++ {
		rows = append(rows, ScreenElement{ScreenBase: ScreenBase{Comp: "card", Text: "Book\nwaiting"}})
	}
	rows = append(rows, ScreenElement{ScreenBase: ScreenBase{Comp: "navbar", WBUI: WBUI{Options: []string{"Home", "Library"}}}})
	res, err := s.DrawScreens(ctx, owner, p.Slug, "", "", "", ScreensInput{Prototype: "App", Screens: []ScreenInput{{Name: "Home", Items: rows}}})
	if err != nil {
		t.Fatal(err)
	}
	wb, _ := WhiteboardOf(*res.Document)
	var frame, title, nav WBElement
	for _, e := range wb.Elements {
		switch {
		case e.Screen != nil:
			frame = e
		case e.Type == "text":
			title = e
		case e.UI != nil && e.UI.Comp == "navbar":
			nav = e
		}
	}
	if frame.H <= 844 || frame.Screen.View != 844 {
		t.Fatalf("frame did not grow: %v %+v", frame.H, frame.Screen)
	}
	if nav.Y+nav.H != frame.Y+frame.H {
		t.Fatalf("navbar not at the bottom: %v %v", nav.Y+nav.H, frame.Y+frame.H)
	}
	if title.TextStyle != "title" || title.FontSize != 28 || title.Align != "left" {
		t.Fatalf("title: %+v", title)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings: %v", res.Warnings)
	}
}
