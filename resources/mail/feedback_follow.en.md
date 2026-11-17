Subject: {{.Portal}}: {{.Title}}{{if .New}} – you follow this topic{{end}}

Hello,
{{if .New}}
you now follow "{{.Title}}" at {{.Portal}}. You get an e-mail when its state or the team's answer changes.
{{else}}
there is news on "{{.Title}}" at {{.Portal}}. The state is now: {{.State}}.{{if .FixedIn}} Fixed in version {{.FixedIn}}.{{end}}
{{if .Answer}}
The team writes:

{{.Answer}}
{{end}}{{end}}
See the topic: {{.Link}}

Stop following: {{.Unsubscribe}}
