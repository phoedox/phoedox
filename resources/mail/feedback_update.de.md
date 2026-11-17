Subject: Neues zu deiner Meldung an {{.Portal}}: {{.Title}}

Hallo,

es gibt Neuigkeiten zu „{{.Title}}“ bei {{.Portal}}. Der Stand ist jetzt: {{.State}}.{{if .FixedIn}} Behoben in Version {{.FixedIn}}.{{end}}
{{if .Answer}}
{{if .Auto}}Eine automatische Antwort aus einer früheren Meldung zum selben Thema:{{else}}Das Team schreibt:{{end}}

{{.Answer}}
{{end}}{{if .Question}}
Das Team hat eine Frage – bitte beantworte sie über den Statuslink aus deiner ersten E-Mail:

{{.Question}}
{{end}}
Details siehst du mit dem Statuslink aus deiner ersten E-Mail.
