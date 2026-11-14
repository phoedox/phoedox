Subject: {{if .Failed}}Überwachung schlägt fehl{{else}}Überwachung wieder in Ordnung{{end}}: {{.Check}}

Hallo {{.Name}},

{{if .Failed}}die Überwachung „{{.Check}}“ im Projekt {{.Project}} ist seit {{.Time}} fehlgeschlagen.{{else}}die Überwachung „{{.Check}}“ im Projekt {{.Project}} ist seit {{.Time}} wieder in Ordnung.{{end}}
{{if .Detail}}
Ergebnis: {{.Detail}}
{{end}}
Zur Seite mit dem Request-Block:
{{.Link}}

Du bekommst diese Nachricht, weil du die Überwachung eingerichtet hast.
