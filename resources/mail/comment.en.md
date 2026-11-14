Subject: {{if .Mentioned}}{{.Author}} mentioned you{{else if .Reply}}New reply from {{.Author}}{{else}}New comment from {{.Author}}{{end}}: {{.Title}}

Hello {{.Name}},

{{if .Mentioned}}{{.Author}} mentioned you in a comment on the page "{{.Title}}" in project {{.Project}}:{{else}}{{.Author}} replied in a discussion on the page "{{.Title}}" in project {{.Project}}:{{end}}

{{.Body}}

Reply or mark as done:
{{.Link}}
