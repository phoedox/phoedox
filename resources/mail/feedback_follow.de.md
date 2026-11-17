Subject: {{.Portal}}: {{.Title}}{{if .New}} – du folgst diesem Thema{{end}}

Hallo,
{{if .New}}
du folgst jetzt „{{.Title}}“ bei {{.Portal}}. Du bekommst eine E-Mail, wenn sich der Stand oder die Antwort des Teams ändert.
{{else}}
es gibt Neues zu „{{.Title}}“ bei {{.Portal}}. Der Stand ist jetzt: {{.State}}.{{if .FixedIn}} Behoben in Version {{.FixedIn}}.{{end}}
{{if .Answer}}
Das Team schreibt:

{{.Answer}}
{{end}}{{end}}
Zum Thema: {{.Link}}

Nicht mehr folgen: {{.Unsubscribe}}
