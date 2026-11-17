Subject: Update on your report to {{.Portal}}: {{.Title}}

Hello,

there is news on "{{.Title}}" at {{.Portal}}. The state is now: {{.State}}.{{if .FixedIn}} Fixed in version {{.FixedIn}}.{{end}}
{{if .Answer}}
The team writes:

{{.Answer}}
{{end}}
You can see the details with the status link from your first e-mail.
