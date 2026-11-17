**+{{.New}} new report(s)** in Phoedox ({{.Total}} in total).

{{range .Excerpts}}> **{{.Title}}**{{if .Type}} ({{.Type}}){{end}}
{{if .Body}}> {{.Body}}
{{end}}{{if .Build}}> Build {{.Build}}
{{end}}
{{end}}[Open the topic in Phoedox]({{.Link}})
