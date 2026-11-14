Subject: Phoedox: {{len .Items}} new notifications

Hello {{.Name}},

this happened since the last digest:
{{range .Items}}
• {{.Headline}}{{if .Body}}
  {{.Body}}{{end}}
  {{.Link}}
{{end}}
You get this digest once a day. You can change that in your profile under "Notifications".
