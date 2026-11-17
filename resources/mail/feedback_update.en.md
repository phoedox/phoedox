Subject: Update on your report to {{.Portal}}: {{.Title}}

Hello,

there is news on "{{.Title}}" at {{.Portal}}. The state is now: {{.State}}.{{if .FixedIn}} Fixed in version {{.FixedIn}}.{{end}}
{{if .Answer}}
{{if .Auto}}An automatic answer from an earlier report on the same issue:{{else}}The team writes:{{end}}

{{.Answer}}
{{end}}{{if .Question}}
The team has a question – please answer it with the status link from your first e-mail:

{{.Question}}
{{end}}
You can see the details with the status link from your first e-mail.
