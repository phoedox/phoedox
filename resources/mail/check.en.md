Subject: {{if .Failed}}Check failing{{else}}Check back to normal{{end}}: {{.Check}}

Hello {{.Name}},

{{if .Failed}}the check "{{.Check}}" in project {{.Project}} has been failing since {{.Time}}.{{else}}the check "{{.Check}}" in project {{.Project}} is back to normal since {{.Time}}.{{end}}
{{if .Detail}}
Result: {{.Detail}}
{{end}}
Page with the request block:
{{.Link}}

You get this message because you set up this check.
