**+{{.New}} neue Meldung(en)** in Phoedox (insgesamt {{.Total}}).

{{range .Excerpts}}> **{{.Title}}**{{if .Type}} ({{.Type}}){{end}}
{{if .Body}}> {{.Body}}
{{end}}{{if .Build}}> Build {{.Build}}
{{end}}
{{end}}[Thema in Phoedox öffnen]({{.Link}})
