Subject: Neues zu deiner Meldung an {{.Portal}}: {{.Title}}

Hallo,

es gibt Neuigkeiten zu „{{.Title}}“ bei {{.Portal}}. Der Stand ist jetzt: {{.State}}.{{if .FixedIn}} Behoben in Version {{.FixedIn}}.{{end}}
{{if .Answer}}
Das Team schreibt:

{{.Answer}}
{{end}}
Details siehst du mit dem Statuslink aus deiner ersten E-Mail.
