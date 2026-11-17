package publish

import (
	"strings"

	"github.com/phoedox/phoedox/internal/store"
	"testing"
)

func TestMarkdown(t *testing.T) {
	md := strings.Join([]string{
		"# Titel", "",
		"> [!warning]", "> Achtung **fett**", "",
		"> [!NOTE]", "> GitHub-Schreibweise", "",
		"> [!callout icon=User color=orange iconcolor=red]", "> Wichtig", "",
		"Text [rot]{color=red} und ++unter++, Formel $a_1^2$ aber $5 und $10. `[x]{color=red}` bleibt.",
		"Siehe [Plan](/p/haus/d/plan) und ![Bild](/api/v1/files/ABC123/foto.png).", "",
		"| a | b |", "|---|---|", "| 1 | 2 |", "",
		"```math", "x^2", "```", "",
		"```embed", "https://youtu.be/dQw4w9WgXcQ", "```", "",
		"```board", "umbau", "```", "",
		"```sh", "echo <b>", "```", "",
		"```mermaid", "flowchart LR", "  A --> B", "```",
		"```chart", `{"points":[{"x":1,"y":2}]}`, "```",
		"<script>alert(1)</script><img src=x onerror=alert(1)>",
		`Am <time datetime="2026-12-24" data-kind="countdown">24.12.</time>.`,
	}, "\n")
	out := Markdown(md, Links{
		Page:  func(p, d string) string { return "/pub/docs/" + p + "/" + d },
		File:  "/pub/docs/_files/",
		Board: func(ref string) string { return `<div class="board">` + ref + `</div>` },
	})
	for _, want := range []string{
		`<div class="callout cb-orange" data-kind="warning"><span class="ci" data-icon="TriangleAlert"></span>`,
		"<strong>fett</strong>",
		`<div class="callout cb-blue" data-kind="note"><span class="ci" data-icon="Info"></span>`,
		`<span class="ci ic-red" data-icon="User">`,
		`<span class="tc-red">rot</span>`, "<u>unter</u>", `<phx-math data-tex="a_1^2">`, "$5 und $10",
		"<code>[x]{color=red}</code>", `href="/pub/docs/haus/plan"`, `src="/pub/docs/_files/ABC123/foto.png"`,
		"<table>", `data-display="1"`, `<phx-embed data-url="https://youtu.be/dQw4w9WgXcQ" data-kind="embed">`,
		`<div class="board">umbau</div>`, "echo &lt;b&gt;", "<phx-mermaid><pre>flowchart LR", `<phx-chart><pre>{&#34;points&#34;`, `<time datetime="2026-12-24" data-kind="countdown">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	for _, bad := range []string{"<script", "onerror", "alert(1)</script>"} {
		if strings.Contains(out, bad) {
			t.Errorf("unsafe %q in output", bad)
		}
	}
}

func TestIncludeAndQueryBlocks(t *testing.T) {
	links := Links{Include: func(ref string) string { return `<div class="include">` + ref + `</div>` }}
	out := Markdown("A\n\n```include\nsafety\n```\n\n```query\ndebian\n```\n\nB", links)
	if !strings.Contains(out, `<div class="include">safety</div>`) || strings.Contains(out, "debian") {
		t.Fatalf("blocks: %s", out)
	}
	if out := Markdown("```include\nsafety\n```", Links{}); strings.Contains(out, "safety") {
		t.Fatalf("include without a resolver: %s", out)
	}
}

func TestWikiLinks(t *testing.T) {
	links := Links{PageRef: func(ref string) string {
		if ref == "D-321" || ref == "ADR-0255" {
			return "/pub/docs/somaro/d-321"
		}
		return ""
	}}
	out := Markdown("Siehe [[D-321]], [[ADR-0255|die Swift-Brücke]] und [[fehlt]]. `[[code]]` bleibt.", links)
	for _, want := range []string{`href="/pub/docs/somaro/d-321" rel="nofollow">D-321</a>`, `href="/pub/docs/somaro/d-321" rel="nofollow">die Swift-Brücke</a>`, `<span class="wikilink">fehlt</span>`, `<code>[[code]]</code>`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
}

func TestMatchDoc(t *testing.T) {
	docs := []store.Document{
		{Slug: "netz", Title: "Netz"},
		{Slug: "swift", Title: "D-321 · iOS project and Swift bridge", Type: store.DocADR, Number: 255},
		{Slug: "d-3210", Title: "D-3210 · Something else"},
	}
	for ref, want := range map[string]string{"netz": "netz", "NETZ": "netz", "ADR-0255": "swift", "adr-255": "swift", "D-321": "swift", "d-3210": "d-3210"} {
		if d, ok := matchDoc(docs, ref); !ok || d.Slug != want {
			t.Errorf("%s: got %q %v, want %q", ref, d.Slug, ok, want)
		}
	}
	if _, ok := matchDoc(docs, "D-32"); ok {
		t.Error("prefix of an identifier must not match")
	}
}

func TestStructureBlocks(t *testing.T) {
	md := strings.Join([]string{
		"> [!faq]- Why does the NAS sleep?", "> Because of **power saving**.", "",
		"> [!tab] Windows", "> Press Win.", "", "> [!tab] macOS", "> Press Cmd.", "",
		"After the tabs.", "",
		"> [!steps id=s1] Setup", "> 1. Plug in", "> 2. Switch on", "",
		"> [!note] With a title", "> Body.", "",
		"```compare", "--- old", "a", "+++ new", "b", "```",
	}, "\n")
	out := Markdown(md, Links{})
	for _, want := range []string{
		`<details class="callout fold cb-gray" data-kind="faq"><summary>`, `<span class="ct">Why does the NAS sleep?</span>`, `<strong>power saving</strong>`,
		"<phx-tabs>\n<section class=\"tab\" data-title=\"Windows\"><h4 class=\"tab-h\">Windows</h4>", `data-title="macOS"`, "</section>\n</phx-tabs>",
		"<phx-steps data-key=\"s1\">\n<div class=\"callout cb-blue\" data-kind=\"steps\">", `<div class="ct">Setup</div>`, `<ol>`,
		`<div class="ct">With a title</div>`, `<phx-compare><pre>--- old`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
	if strings.Count(out, "<phx-tabs>") != 1 || !strings.Contains(out, "<p>After the tabs.</p>") || strings.Contains(out, "<p><phx-steps") {
		t.Errorf("tab group:\n%s", out)
	}
}
