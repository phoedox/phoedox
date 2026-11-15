package importer

import (
	"strings"
	"testing"
)

func TestArchiveMarkdownImages(t *testing.T) {
	a, err := ReadArchive(zipOf(t, map[string]string{
		"Vault/Start.md":             "# Start\n\nSee [the router](Netz/Router.md) and ![photo](img/rack.png) and ![[foto.png]].\n![remote](https://x.org/a.png)",
		"Vault/Netz/Router.md":       "# Router\n",
		"Vault/img/rack.png":         "png1",
		"Vault/attachments/foto.png": "png2",
		"Vault/logo.svg":             "<svg/>",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if a.Format != "markdown" || len(a.Pages) != 2 || len(a.Assets) != 2 {
		t.Fatalf("%+v", a)
	}
	var start Page
	for _, p := range a.Pages {
		if p.Title == "Start" {
			start = p
		}
	}
	for _, want := range []string{"[the router](page:Vault/Netz/Router.md)", "![photo](asset:Vault/img/rack.png)", "![foto.png](asset:Vault/attachments/foto.png)", "(https://x.org/a.png)"} {
		if !strings.Contains(start.Markdown, want) {
			t.Fatalf("missing %q in %s", want, start.Markdown)
		}
	}
	if len(a.Skipped) != 1 || a.Skipped[0] != "Vault/logo.svg" {
		t.Fatalf("skipped: %v", a.Skipped)
	}
	out := ReplaceRefs(start.Markdown, func(p string) string { return "/p/x/d/router" }, func(p string) string { return "/api/v1/files/" + p })
	if !strings.Contains(out, "[the router](/p/x/d/router)") || !strings.Contains(out, "(/api/v1/files/Vault/img/rack.png)") {
		t.Fatalf("refs: %s", out)
	}
}

func TestArchiveNotion(t *testing.T) {
	a, err := ReadArchive(zipOf(t, map[string]string{
		"Export/Home 0123456789abcdef0123456789abcdef.md":                                                  "# Home\n\nGo to [Network](Home%200123456789abcdef0123456789abcdef/Network%20fedcba9876543210fedcba9876543210.md).\n",
		"Export/Home 0123456789abcdef0123456789abcdef/Network fedcba9876543210fedcba9876543210.md":         "# Network\n\n![](Network%20fedcba9876543210fedcba9876543210/switch.jpg)",
		"Export/Home 0123456789abcdef0123456789abcdef/Network fedcba9876543210fedcba9876543210/switch.jpg": "jpg",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if a.Format != "notion" || len(a.Pages) != 2 || len(a.Assets) != 1 {
		t.Fatalf("%+v", a)
	}
	for _, p := range a.Pages {
		if p.Title == "Network" {
			if p.Category != "Home" || strings.HasPrefix(p.Markdown, "# Network") || !strings.Contains(p.Markdown, "](asset:") {
				t.Fatalf("network: %+v", p)
			}
		} else if !strings.Contains(p.Markdown, "[Network](page:") {
			t.Fatalf("home: %+v", p)
		}
	}
}

func TestArchiveConfluence(t *testing.T) {
	page := `<html><head><title>Infra : Backup plan</title></head><body>
<div id="breadcrumb-section"><ol id="breadcrumbs"><li><a href="index.html">Infra</a></li><li><a href="Servers_1.html">Servers</a></li></ol></div>
<h1 id="title-heading"><span id="title-text">Infra : Backup plan</span></h1>
<div id="main-content" class="wiki-content group">
<h2>Targets</h2><p>Nightly to the <strong>NAS</strong>, see <a href="Servers_1.html">Servers</a>.</p>
<ul><li>restic</li><li>rclone<ul><li>B2</li></ul></li></ul>
<table><tr><th>Host</th><th>When</th></tr><tr><td>nas</td><td>02:00</td></tr></table>
<p><img src="attachments/42/diagram.png" alt="Diagram"></p>
<pre class="syntaxhighlighter-pre" data-syntaxhighlighter-params="brush: bash; gutter: false">restic backup /data</pre>
</div><div id="footer">Confluence</div></body></html>`
	a, err := ReadArchive(zipOf(t, map[string]string{
		"INFRA/index.html":                 "<html>Space</html>",
		"INFRA/Backup-plan_2.html":         page,
		"INFRA/Servers_1.html":             `<html><head><title>Infra : Servers</title></head><body><div id="main-content"><p>Hosts</p></div></body></html>`,
		"INFRA/attachments/42/diagram.png": "png",
		"INFRA/styles/site.css":            "x",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if a.Format != "confluence" || len(a.Pages) != 2 || len(a.Assets) != 1 {
		t.Fatalf("%+v", a)
	}
	var p Page
	for _, x := range a.Pages {
		if x.Title == "Backup plan" {
			p = x
		}
	}
	for _, want := range []string{"## Targets", "**NAS**", "[Servers](page:INFRA/Servers_1.html)", "- restic", "  - B2", "| Host | When |", "| nas | 02:00 |", "![Diagram](asset:INFRA/attachments/42/diagram.png)", "```bash\nrestic backup /data\n```"} {
		if !strings.Contains(p.Markdown, want) {
			t.Fatalf("missing %q in:\n%s", want, p.Markdown)
		}
	}
	if p.Category != "Servers" || strings.Contains(p.Markdown, "Confluence") {
		t.Fatalf("category/footer: %+v", p)
	}
}

func TestArchiveBookStack(t *testing.T) {
	data := `{"instance":{"id":"x"},"book":{"name":"Homelab","pages":[{"id":1,"name":"Intro","markdown":"See [[[bsexport:page:2]]]([[bsexport:page:2]]) ![rack]([[bsexport:image:7]])","images":[{"id":7,"name":"rack","file":"rack.png"}]}],
"chapters":[{"id":3,"name":"Network","pages":[{"id":2,"name":"Router","markdown":"","html":"<p>The <em>router</em> <img src=\"[[bsexport:image:8]]\"></p>","images":[{"id":8,"name":"r","file":"r.jpg"}]}]}]}}`
	a, err := ReadArchive(zipOf(t, map[string]string{"data.json": data, "files/rack.png": "png", "files/r.jpg": "jpg"}))
	if err != nil {
		t.Fatal(err)
	}
	if a.Format != "bookstack" || len(a.Pages) != 2 || len(a.Assets) != 2 {
		t.Fatalf("%+v", a)
	}
	for _, p := range a.Pages {
		switch p.Title {
		case "Intro":
			if !strings.Contains(p.Markdown, "](page:bookstack/2)") || !strings.Contains(p.Markdown, "![rack](asset:files/rack.png)") {
				t.Fatalf("intro: %s", p.Markdown)
			}
		case "Router":
			if p.Category != "Network" || !strings.Contains(p.Markdown, "_router_") || !strings.Contains(p.Markdown, "(asset:files/r.jpg)") {
				t.Fatalf("router: %+v", p)
			}
		}
	}
}
