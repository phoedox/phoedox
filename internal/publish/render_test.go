package publish

import (
	"strings"
	"testing"
)

func TestMarkdown(t *testing.T) {
	md := strings.Join([]string{
		"# Titel", "",
		"> [!warning]", "> Achtung **fett**", "",
		"> [!callout icon=User color=orange iconcolor=red]", "> Wichtig", "",
		"Text [rot]{color=red} und ++unter++, Formel $a_1^2$ aber $5 und $10. `[x]{color=red}` bleibt.",
		"Siehe [Plan](/p/haus/d/plan) und ![Bild](/api/v1/files/ABC123/foto.png).", "",
		"| a | b |", "|---|---|", "| 1 | 2 |", "",
		"```math", "x^2", "```", "",
		"```embed", "https://youtu.be/dQw4w9WgXcQ", "```", "",
		"```board", "umbau", "```", "",
		"```sh", "echo <b>", "```", "",
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
		`<span class="ci ic-red" data-icon="User">`,
		`<span class="tc-red">rot</span>`, "<u>unter</u>", `<phx-math data-tex="a_1^2">`, "$5 und $10",
		"<code>[x]{color=red}</code>", `href="/pub/docs/haus/plan"`, `src="/pub/docs/_files/ABC123/foto.png"`,
		"<table>", `data-display="1"`, `<phx-embed data-url="https://youtu.be/dQw4w9WgXcQ" data-kind="embed">`,
		`<div class="board">umbau</div>`, "echo &lt;b&gt;", `<time datetime="2026-12-24" data-kind="countdown">`,
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
