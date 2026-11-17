{{if .Summary}}{{.Summary}}

{{end}}**{{.Reports}} report(s)** from the feedback portal "{{.Portal}}"{{if .Category}} · category {{.Category}}{{end}}{{if .Tags}} · tags {{.Tags}}{{end}} · priority {{.Priority}}

{{range .Excerpts}}> **{{.Title}}**{{if .Type}} ({{.Type}}){{end}}
{{if .Body}}> {{.Body}}
{{end}}{{if .Build}}> Build {{.Build}}
{{end}}
{{end}}[Open the topic in Phoedox]({{.Link}}){{if .PublicLink}} · [public page]({{.PublicLink}}){{end}}

_Forwarded from Phoedox. New reports are added here as comments; when this issue is closed, the topic counts as done there. Reporters stay private._
