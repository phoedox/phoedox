package store

import (
	"context"
	"testing"
)

func TestDocumentsContaining(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, _ := d.CreateUser(ctx, User{ID: id(), Email: "c@y.de", Name: "C"})
			p, err := d.CreateProject(ctx, Project{ID: id(), Slug: "p", Name: "P", Short: "P", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u.ID})
			if err != nil {
				t.Fatal(err)
			}
			for i, c := range []string{`{"markdown":"` + "```gantt" + `\nA: 2026-01-01\n` + "```" + `"}`, `{"elements":[{"type":"block","kind":"gantt"}]}`, `{"markdown":"100% done_now"}`, `{"markdown":"nothing"}`} {
				if _, err := d.CreateDocument(ctx, Document{ID: id(), ProjectID: p.ID, Type: DocMarkdown, Slug: string(rune('a' + i)), Title: "T", Content: JSON(c), CreatedBy: u.ID}, "user:"+u.ID); err != nil {
					t.Fatal(err)
				}
			}
			if docs, err := d.DocumentsContaining(ctx, []string{"gantt"}); err != nil || len(docs) != 2 || len(docs[0].Content) == 0 {
				t.Fatalf("gantt: %v %d", err, len(docs))
			}
			// LIKE wildcards in the words are literal.
			if docs, err := d.DocumentsContaining(ctx, []string{"0%", "e_n"}); err != nil || len(docs) != 1 {
				t.Fatalf("escaped: %v %d", err, len(docs))
			}
			if docs, _ := d.DocumentsContaining(ctx, nil); docs != nil {
				t.Fatal("no words, no documents")
			}
		})
	}
}
