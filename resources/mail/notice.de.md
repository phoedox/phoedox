Subject: {{if eq .Kind "comment.mention"}}{{.Actor}} hat dich erwähnt{{else if eq .Kind "comment.reply"}}Neue Antwort von {{.Actor}}{{else if eq .Kind "check.fail"}}Überwachung schlägt fehl{{else if eq .Kind "check.recover"}}Überwachung wieder in Ordnung{{else if eq .Kind "task.assigned"}}{{.Actor}} hat dir eine Aufgabe zugewiesen{{else if eq .Kind "project.summary"}}Wochenrückblick{{else}}Testnachricht von Phoedox{{end}}: {{.Title}}

Hallo {{.Name}},

{{if eq .Kind "task.assigned"}}{{.Actor}} hat dir im Projekt {{.Project}} die Aufgabe „{{.Title}}“ zugewiesen.{{else if eq .Kind "test"}}so sehen Benachrichtigungen von Phoedox aus. Diese Nachricht hast du selbst ausgelöst.{{else}}{{.Title}} ({{.Project}}){{end}}
{{if .Body}}
{{.Body}}
{{end}}
{{.Link}}

Einstellungen für Benachrichtigungen findest du in deinem Profil.
