Subject: Phoedox: {{len .Items}} neue Benachrichtigungen

Hallo {{.Name}},

das ist seit der letzten Sammelmail passiert:
{{range .Items}}
• {{.Headline}}{{if .Body}}
  {{.Body}}{{end}}
  {{.Link}}
{{end}}
Du bekommst diese Sammelmail einmal am Tag. Ändern kannst du das in deinem Profil unter „Benachrichtigungen“.
