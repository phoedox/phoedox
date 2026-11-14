Subject: {{if .Mentioned}}{{.Author}} hat dich erwähnt{{else if .Reply}}Neue Antwort von {{.Author}}{{else}}Neuer Kommentar von {{.Author}}{{end}}: {{.Title}}

Hallo {{.Name}},

{{if .Mentioned}}{{.Author}} hat dich in einem Kommentar zur Seite „{{.Title}}“ im Projekt {{.Project}} erwähnt:{{else}}{{.Author}} hat in einer Diskussion zur Seite „{{.Title}}“ im Projekt {{.Project}} geantwortet:{{end}}

{{.Body}}

Antworten oder als erledigt markieren:
{{.Link}}
