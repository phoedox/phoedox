package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

// TestWhiteboard covers ADR-0068: validation, auto-publish with one version
// per person and window, live access (viewers read-only), mentions.
func TestWhiteboard(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Board"})
	if _, err := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "device", Name: "NAS", Slug: "nas"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMember(ctx, owner, p.Slug, reader.User.Email, "reader"); err != nil {
		t.Fatal(err)
	}
	half := 0.5
	bad := []Whiteboard{
		{Elements: []WBElement{{ID: "a", Type: "blob"}}},
		{Elements: []WBElement{{ID: "a", Type: "rect"}, {ID: "a", Type: "rect"}}},
		{Elements: []WBElement{{ID: "a", Type: "rect", Stroke: "#ff0000"}}},
		{Elements: []WBElement{{ID: "a", Type: "arrow", Points: [][2]float64{{0, 0}}}}},
		{Elements: []WBElement{{ID: "a", Type: "arrow", Points: [][2]float64{{0, 0}, {1, 1}}, Start: &WBBinding{Element: "missing"}}}},
		{Elements: []WBElement{{ID: "b", Type: "rect"}, {ID: "a", Type: "arrow", Points: [][2]float64{{0, 0}, {1, 1}}, Start: &WBBinding{Element: "b", FX: &half}}}},
		{Elements: []WBElement{{ID: "a", Type: "embed", URL: "javascript:alert(1)", Kind: "link"}}},
		{Elements: []WBElement{{ID: "a", Type: "image", File: "nope"}}},
		{Elements: []WBElement{{ID: "a", Type: "rect", X: 1e9}}},
		{Background: "stars"},
	}
	for i, wb := range bad {
		if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "whiteboard", Title: "X", Whiteboard: &wb}); invalidKey(err) != "validation.whiteboard" {
			t.Errorf("bad %d: %v", i, err)
		}
	}

	wb := Whiteboard{Elements: []WBElement{
		{ID: "box", Type: "rect", X: 0, Y: 0, W: 200, H: 80, Text: "Backup @nas", Stroke: "blue"},
		{ID: "cloud", Type: "ellipse", X: 400, Y: 0, W: 160, H: 80, Text: "Storage box"},
		{ID: "a1", Type: "arrow", Points: [][2]float64{{0, 0}, {300, 40}, {0, 0}}, Start: &WBBinding{Element: "box"}, End: &WBBinding{Element: "cloud", FX: &half, FY: &half}, Round: true},
		{ID: "yt", Type: "embed", X: 0, Y: 200, W: 320, H: 200, URL: "https://www.youtube.com/watch?v=x", Kind: "video", Preview: &WBPreview{Title: "Restic intro"}},
	}, Comments: []WBComment{{ID: "c1", X: 10, Y: 10, Element: "box", Text: "Encrypt first?"}}}
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "whiteboard", Title: "Backup sketch", Whiteboard: &wb})
	if err != nil || !res.Applied {
		t.Fatalf("create: %v", err)
	}
	doc := res.Document
	got, err := WhiteboardOf(*doc)
	if err != nil || len(got.Elements) != 4 || got.Version != 1 {
		t.Fatalf("read back: %v %+v", err, got)
	}
	// The @mention in a shape links the entry.
	if docs, _ := s.DB.DocumentsMentioning(ctx, entityID(t, s, owner, p.Slug, "nas")); len(docs) != 1 {
		t.Fatalf("mention not linked: %v", docs)
	}

	// Live saves: published at once; the same person within the window
	// updates one version.
	wb.Elements[0].Text = "Nightly backup @nas"
	d1, err := s.SaveWhiteboard(ctx, owner, p.Slug, doc.Slug, &wb)
	if err != nil {
		t.Fatal(err)
	}
	wb.Elements[1].Text = "Storage box (EU)"
	d2, err := s.SaveWhiteboard(ctx, owner, p.Slug, doc.Slug, &wb)
	if err != nil || d2.Version != d1.Version || d1.Version != doc.Version+1 {
		t.Fatalf("versions: %d %d %d %v", doc.Version, d1.Version, d2.Version, err)
	}
	cur, _ := s.Document(ctx, owner, p.Slug, doc.Slug)
	if !strings.Contains(string(cur.Content), "Storage box (EU)") {
		t.Fatalf("not published: %s", cur.Content)
	}
	// Only the browser saves live, and readers cannot.
	if _, err := s.SaveWhiteboard(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug, doc.Slug, &wb); !errors.Is(err, ErrForbidden) {
		t.Fatalf("mcp live save: %v", err)
	}
	if _, err := s.SaveWhiteboard(ctx, reader, p.Slug, doc.Slug, &wb); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader save: %v", err)
	}
	// Live: editors join read-write without a draft, readers watch.
	if _, ro, err := s.LiveSession(ctx, owner, p.Slug, doc.Slug); err != nil || ro {
		t.Fatalf("owner live: %v %v", ro, err)
	}
	if _, ro, err := s.LiveSession(ctx, reader, p.Slug, doc.Slug); err != nil || !ro {
		t.Fatalf("reader live: %v %v", ro, err)
	}
	// A comment pin on a deleted element stays where it is.
	wb.Elements = wb.Elements[1:]
	wb.Elements[1].Start = nil
	if _, err := s.SaveWhiteboard(ctx, owner, p.Slug, doc.Slug, &wb); err != nil {
		t.Fatal(err)
	}
	got, _ = WhiteboardOf(mustDoc(t, s, owner, p.Slug, doc.Slug))
	if got.Comments[0].Element != "" {
		t.Fatalf("dangling comment: %+v", got.Comments[0])
	}
	// Changes through the API (e.g. the AI) make a normal version.
	wb.Background = "dots"
	r2, err := s.UpdateDocument(ctx, owner, p.Slug, doc.Slug, DocumentPatch{Whiteboard: &wb})
	if err != nil || r2.Document.Version != d2.Version+1 {
		t.Fatalf("api update: %v", err)
	}
}

func TestEmbedKind(t *testing.T) {
	for u, want := range map[string]string{
		"https://www.youtube.com/watch?v=1":             "video",
		"https://youtu.be/abc":                          "video",
		"https://github.com/o/r/pull/4":                 "code",
		"https://git.example.org/family/infra/pulls/42": "code",
		"https://github.com/o/r":                        "link",
		"https://restic.readthedocs.io/":                "link",
	} {
		if got := EmbedKind(u); got != want {
			t.Errorf("%s: %s, want %s", u, got, want)
		}
	}
}

func entityID(t *testing.T, s *Service, a Actor, project, slug string) string {
	t.Helper()
	e, err := s.Entity(ctx0, a, project, slug)
	if err != nil {
		t.Fatal(err)
	}
	return e.Entity.ID
}

func mustDoc(t *testing.T, s *Service, a Actor, project, slug string) store.Document {
	t.Helper()
	d, err := s.Document(ctx0, a, project, slug)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

var ctx0 = context.Background()

// TestWhiteboardAI covers the AI tools: diagrams laid out by Phoedox,
// operations, answers to comments, the compact reading, write modes.
func TestWhiteboardAI(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "AI Board", AIWriteMode: "direct"})
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "t"}
	in := DiagramInput{Title: "Backup", Nodes: []DiagramNode{
		{ID: "laptop", Text: "Laptops", Shape: "ellipse"}, {ID: "nas", Text: "NAS", Group: "home"},
		{ID: "box", Text: "Storage box", Color: "green"}, {ID: "phone", Text: "Phones", Group: "home"},
	}, Edges: []DiagramEdge{{From: "laptop", To: "nas", Label: "Time Machine"}, {From: "phone", To: "nas"}, {From: "nas", To: "box", Dashed: true}, {From: "box", To: "laptop"}},
		Groups: []DiagramGroup{{ID: "home", Label: "At home"}}}
	if _, err := s.DrawDiagram(ctx, ai, p.Slug, "", "", "", DiagramInput{Nodes: []DiagramNode{{ID: "a"}}, Edges: []DiagramEdge{{From: "a", To: "x"}}}); invalidKey(err) != "validation.diagram" {
		t.Fatalf("unknown node: %v", err)
	}
	res, err := s.DrawDiagram(ctx, ai, p.Slug, "", "Backup sketch", "", in)
	if err != nil || !res.Applied {
		t.Fatalf("draw: %v %+v", err, res)
	}
	v, err := s.WhiteboardView(ctx, ai, p.Slug, res.Document.Slug)
	if err != nil {
		t.Fatal(err)
	}
	box := map[string][]int{}
	arrows := 0
	for _, e := range v.Elements {
		box[e.ID] = e.Box
		if e.Type == "arrow" {
			arrows++
			if e.From == "" || e.To == "" {
				t.Fatalf("unbound arrow %+v", e)
			}
		}
	}
	if arrows != 4 || len(v.Frames) != 2 {
		t.Fatalf("elements: %d arrows, frames %+v", arrows, v.Frames)
	}
	// Layers left to right; the cycle box → laptop is broken where the
	// search from the source (phone) closes it.
	if !(box["n-phone"][0] < box["n-nas"][0] && box["n-nas"][0] < box["n-box"][0] && box["n-box"][0] < box["n-laptop"][0]) {
		t.Fatalf("layout: %v", box)
	}
	home := v.Frames[1]
	if home.Title != "At home" || len(home.Elements) != 2 {
		t.Fatalf("group frame: %+v", home)
	}
	// A second diagram goes right of the first.
	res2, err := s.DrawDiagram(ctx, ai, p.Slug, res.Document.Slug, "", "", DiagramInput{Nodes: []DiagramNode{{ID: "x", Text: "Later"}}})
	if err != nil {
		t.Fatal(err)
	}
	v, _ = s.WhiteboardView(ctx, ai, p.Slug, res2.Document.Slug)
	for _, e := range v.Elements {
		if e.ID == "n-x" && e.Box[0] <= v.Extent[0]+300 {
			t.Fatalf("second diagram overlaps: %v %v", e.Box, v.Extent)
		}
	}

	// Operations: add, update (null removes), delete unbinds.
	ops := []WBOp{
		{Op: "add", Element: json.RawMessage(`{"id":"note","type":"sticky","x":0,"y":400,"text":"Test restore"}`)},
		{Op: "update", ID: "n-box", Element: json.RawMessage(`{"text":"Storage box (EU)","stroke":null}`)},
		{Op: "delete", ID: "n-nas"},
	}
	if _, err := s.EditWhiteboard(ctx, ai, p.Slug, res.Document.Slug, ops); err != nil {
		t.Fatal(err)
	}
	wb, _ := WhiteboardOf(mustDoc(t, s, owner, p.Slug, res.Document.Slug))
	for _, e := range wb.Elements {
		switch {
		case e.ID == "note" && (e.W == 0 || e.Fill != "yellow" || e.Seed == 0):
			t.Fatalf("defaults: %+v", e)
		case e.ID == "n-box" && (e.Text != "Storage box (EU)" || e.Stroke != ""):
			t.Fatalf("update: %+v", e)
		case e.ID == "n-nas":
			t.Fatal("not deleted")
		case (e.Start != nil && e.Start.Element == "n-nas") || (e.End != nil && e.End.Element == "n-nas"):
			t.Fatalf("still bound: %+v", e)
		}
	}
	if _, err := s.EditWhiteboard(ctx, ai, p.Slug, res.Document.Slug, []WBOp{{Op: "update", ID: "ghost", Element: json.RawMessage(`{}`)}}); invalidKey(err) != "validation.whiteboard_op" {
		t.Fatalf("unknown id: %v", err)
	}

	// Comments: open ones are listed; the AI answers.
	wb.Comments = []WBComment{{ID: "c1", X: 5, Y: 5, Element: "n-box", Name: "Sam", Text: "Encrypt before upload?"}, {ID: "c2", Resolved: true, Text: "old"}}
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, res.Document.Slug, DocumentPatch{Whiteboard: wb}); err != nil {
		t.Fatal(err)
	}
	v, _ = s.WhiteboardView(ctx, ai, p.Slug, res.Document.Slug)
	if len(v.Comments) != 1 || v.Comments[0].ElementText != "Storage box (EU)" {
		t.Fatalf("comments: %+v", v.Comments)
	}
	if _, err := s.ReplyWhiteboardComment(ctx, ai, p.Slug, res.Document.Slug, "c1", "Yes – restic encrypts on the client."); err != nil {
		t.Fatal(err)
	}
	wb, _ = WhiteboardOf(mustDoc(t, s, owner, p.Slug, res.Document.Slug))
	if r := wb.Comments[0].Replies; len(r) != 1 || !strings.HasPrefix(r[0].Name, "AI") {
		t.Fatalf("reply: %+v", wb.Comments[0])
	}

	// In projects with suggestions, an AI drawing becomes a suggestion.
	q, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Careful"})
	r, err := s.DrawDiagram(ctx, ai, q.Slug, "", "Sketch", "", DiagramInput{Nodes: []DiagramNode{{ID: "a", Text: "A"}}})
	if err != nil || r.Applied || r.Suggestion == nil {
		t.Fatalf("suggest mode: %v %+v", err, r)
	}
}

// TestSubtasks: a card's checklist through create and update, validated,
// ids kept, stored in both dialects.
func TestSubtasks(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Tasks"})
	b, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Board", Board: &Board{Columns: []BoardColumn{{ID: "todo", Name: "To do"}, {ID: "done", Name: "Done", Done: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(ctx, owner, p.Slug, b.Document.Slug, TaskInput{Title: "Set up backups", Subtasks: []store.Subtask{{Title: " Buy disk "}, {Title: "Configure restic", Due: "2026-12-01"}}})
	if err != nil || len(task.Subtasks) != 2 || task.Subtasks[0].Title != "Buy disk" || task.Subtasks[0].ID == "" {
		t.Fatalf("create: %v %+v", err, task.Subtasks)
	}
	subs := task.Subtasks
	subs[0].Done = true
	subs = append(subs, store.Subtask{Title: "Test restore"})
	up, err := s.UpdateTask(ctx, owner, p.Slug, task.ID, TaskPatch{Subtasks: &subs})
	if err != nil || len(up.Subtasks) != 3 || !up.Subtasks[0].Done || up.Subtasks[0].ID != task.Subtasks[0].ID {
		t.Fatalf("update: %v %+v", err, up.Subtasks)
	}
	list, _ := s.Tasks(ctx, owner, p.Slug, b.Document.Slug, "", "", false)
	if len(list) != 1 || len(list[0].Subtasks) != 3 {
		t.Fatalf("listed: %+v", list)
	}
	for _, bad := range [][]store.Subtask{{{Title: ""}}, {{Title: "x", Due: "tomorrow"}}} {
		if _, err := s.UpdateTask(ctx, owner, p.Slug, task.ID, TaskPatch{Subtasks: &bad}); err == nil {
			t.Fatalf("bad subtasks accepted: %+v", bad)
		}
	}
	none := []store.Subtask{}
	if up, _ := s.UpdateTask(ctx, owner, p.Slug, task.ID, TaskPatch{Subtasks: &none}); len(up.Subtasks) != 0 {
		t.Fatalf("clear: %+v", up.Subtasks)
	}
}

// Frames of different groups must not overlap, even when the groups share
// a layer (the backup example from the demo).
func TestDiagramGroupsDoNotOverlap(t *testing.T) {
	in := DiagramInput{
		Groups: []DiagramGroup{{ID: "home", Label: "At home"}, {ID: "off", Label: "Off-site"}},
		Nodes: []DiagramNode{{ID: "laptops", Text: "Laptops", Group: "home"}, {ID: "phones", Text: "Phones", Group: "home"}, {ID: "nas", Text: "NAS", Group: "home"},
			{ID: "usb", Text: "USB disk", Group: "home"}, {ID: "box", Text: "Storage box", Group: "off"}, {ID: "note", Text: "Restore test"}},
		Edges: []DiagramEdge{{From: "laptops", To: "nas"}, {From: "phones", To: "nas"}, {From: "nas", To: "usb"}, {From: "nas", To: "box"}, {From: "note", To: "box"}},
	}
	els := layoutDiagram(&Whiteboard{}, in)
	var frames []WBElement
	boxes := map[string]WBElement{}
	for _, e := range els {
		if e.Type == "frame" {
			frames = append(frames, e)
		}
		boxes[e.ID] = e
	}
	if len(frames) != 2 {
		t.Fatalf("frames: %+v", frames)
	}
	a, b := frames[0], frames[1]
	if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
		t.Fatalf("frames overlap: %+v %+v", a, b)
	}
	// Nodes outside a group stay outside every frame.
	n := boxes["n-note"]
	for _, f := range frames {
		if n.X < f.X+f.W && f.X < n.X+n.W && n.Y < f.Y+f.H && f.Y < n.Y+n.H {
			t.Fatalf("ungrouped node inside frame %s", f.Text)
		}
	}
}

func TestDescriptionLinksCardDocs(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Home"})
	adr, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Backup target", ADR: &ADR{Status: "accepted", Context: "x", Decision: "Box"}})
	if err != nil {
		t.Fatal(err)
	}
	page, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Restore", Markdown: "x"})
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Tasks"})
	task, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Choose", Description: "Decide [[ADR-0001]] together, see [[unknown]]."})
	if err != nil {
		t.Fatal(err)
	}
	docs, _ := s.DB.TaskDocs(ctx, p.ID, task.ID)
	if len(docs) != 1 || docs[0].ID != adr.Document.ID {
		t.Fatalf("create: %+v", docs)
	}
	desc := "Decide [[ADR-0001]], then [[restore|the restore page]]."
	if _, err := s.UpdateTask(ctx, owner, p.Slug, task.ID, TaskPatch{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	docs, _ = s.DB.TaskDocs(ctx, p.ID, task.ID)
	if len(docs) != 2 || docs[1].ID != page.Document.ID && docs[0].ID != page.Document.ID {
		t.Fatalf("update: %+v", docs)
	}
}

func TestChecklistBecomesSubtasks(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Home"})
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Tasks"})
	desc := "Part of the plan.\n\n- [x] Create Wi-Fi\n- [ ] Connect the @tv\n\nThen test."
	task, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "TV", Description: desc})
	if err != nil {
		t.Fatal(err)
	}
	if task.Description != "Part of the plan.\n\nThen test." || len(task.Subtasks) != 2 || !task.Subtasks[0].Done || task.Subtasks[1].Title != "Connect the @tv" {
		t.Fatalf("create: %q %+v", task.Description, task.Subtasks)
	}
	more := task.Description + "\n- [ ] Test casting"
	// The web sends the subtasks it shows together with the description.
	shown := task.Subtasks
	u, err := s.UpdateTask(ctx, owner, p.Slug, task.ID, TaskPatch{Description: &more, Subtasks: &shown})
	if err != nil || len(u.Subtasks) != 3 || u.Subtasks[2].Title != "Test casting" || u.Description != "Part of the plan.\n\nThen test." {
		t.Fatalf("update: %q %+v %v", u.Description, u.Subtasks, err)
	}
}
