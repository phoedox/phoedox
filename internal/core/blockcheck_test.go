package core

import (
	"strings"
	"testing"
)

func TestCheckBlocks(t *testing.T) {
	md := "# Page\n\n```mermaid\nsequenceDiagram\n  participant Off\n  A->>end: hi\n  loop every day\n    A->>B: ping\n```\n\n" +
		"```mermaid\nflowchart TD\n  A[Start --> B(Next)\n  subgraph S\n  C>flag]\n```\n\n" +
		"```mermaid\nflowhart TD\n  A --> B\n```\n\n" +
		"```mermaid\nflowchart LR\n  A[One] --> B{Two}\n  subgraph X\n    C\n  end\n```\n\n" +
		"```dbmodel\n-- tables: users, nope\ncreate table sessions (id int primary key, user_id int references users(id), org int references orgs(id));\n```\n"
	ps := CheckBlocks(md, map[string][]string{"users": nil})
	got := map[int][]string{}
	for _, p := range ps {
		got[p.Block] = append(got[p.Block], p.Severity+": "+p.Message)
	}
	j := func(b int) string { return strings.Join(got[b], " | ") }
	if !strings.Contains(j(1), `"Off" is a Mermaid keyword`) || !strings.Contains(j(1), `"end" is a Mermaid keyword`) || !strings.Contains(j(1), "never closed") {
		t.Fatalf("block 1: %s", j(1))
	}
	if !strings.Contains(j(2), "unbalanced [ ]") || !strings.Contains(j(2), "never closed") || strings.Count(j(2), "unbalanced") != 1 {
		t.Fatalf("block 2: %s", j(2))
	}
	if !strings.Contains(j(3), "unknown diagram type") {
		t.Fatalf("block 3: %s", j(3))
	}
	if len(got[4]) != 0 {
		t.Fatalf("block 4 should be fine: %s", j(4))
	}
	if !strings.Contains(j(5), `"nope" is not defined`) || !strings.Contains(j(5), `references "orgs"`) || strings.Contains(j(5), `"users"`) {
		t.Fatalf("block 5: %s", j(5))
	}
	// Line numbers point into the page.
	for _, p := range ps {
		if p.Block == 3 && p.Line != 19 {
			t.Fatalf("line of block 3: %d", p.Line)
		}
	}
}
