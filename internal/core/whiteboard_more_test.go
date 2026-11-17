package core

import (
	"context"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestParseMermaid(t *testing.T) {
	in, err := ParseMermaid(`flowchart TD
  %% a comment
  A[Start] --> B{Is it?}
  B -- Yes --> C(Do it)
  B -.->|No| D((Stop))
  C & D ==> E["End<br>here"]
  subgraph sg [Backend]
    F --- G
  end
  classDef red fill:#f00
  class A red`)
	if err != nil {
		t.Fatal(err)
	}
	if in.Direction != "down" || len(in.Nodes) != 7 || len(in.Groups) != 1 || in.Groups[0].Label != "Backend" {
		t.Fatalf("parse: %+v", in)
	}
	byID := map[string]DiagramNode{}
	for _, n := range in.Nodes {
		byID[n.ID] = n
	}
	if byID["B"].Shape != "diamond" || byID["D"].Shape != "ellipse" || byID["E"].Text != "End\nhere" || byID["F"].Group != "sg" {
		t.Fatalf("nodes: %+v", byID)
	}
	want := map[string]DiagramEdge{}
	for _, e := range in.Edges {
		want[e.From+">"+e.To] = e
	}
	if want["B>C"].Label != "Yes" || want["B>D"].Label != "No" || !want["B>D"].Dashed || want["C>E"].From != "C" || want["D>E"].From != "D" || !want["F>G"].Line || len(in.Edges) != 6 {
		t.Fatalf("edges: %+v", in.Edges)
	}
	if _, err := ParseMermaid("sequenceDiagram\n A->>B: hi"); invalidKey(err) != "validation.mermaid" {
		t.Fatalf("sequence: %v", err)
	}
}

func TestDrawNeighbourhoodAndMermaid(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	for _, n := range []string{"NAS", "Switch", "Router"} {
		if _, err := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "server", Name: n}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Link(ctx, owner, p.Slug, EdgeInput{From: "nas", Rel: "depends_on", To: "switch"}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Link(ctx, owner, p.Slug, EdgeInput{From: "switch", Rel: "depends_on", To: "router"})
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "whiteboard", Title: "Net", Whiteboard: &Whiteboard{Elements: []WBElement{}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DrawNeighbourhood(ctx, owner, p.Slug, res.Document.Slug, "switch"); err != nil {
		t.Fatal(err)
	}
	doc, _ := s.DB.GetDocument(ctx, p.ID, res.Document.ID)
	wb, _ := WhiteboardOf(doc)
	ents, arrows := 0, 0
	for _, e := range wb.Elements {
		if e.Entity != "" {
			ents++
		}
		if e.Type == "arrow" && e.Start != nil && e.End != nil && strings.Contains(e.Text, "depends on") {
			arrows++
		}
	}
	if ents != 3 || arrows != 2 {
		t.Fatalf("neighbourhood: %d entries, %d arrows: %+v", ents, arrows, wb.Elements)
	}
	r2, err := s.DrawMermaid(ctx, owner, p.Slug, "", "Flow", "", "graph LR\n a --> b")
	if err != nil || r2.Document.Type != store.DocWhiteboard {
		t.Fatalf("mermaid: %v %+v", err, r2)
	}
}
