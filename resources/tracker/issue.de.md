{{if .Summary}}{{.Summary}}

{{end}}**{{.Reports}} Meldung(en)** aus dem Feedback-Portal „{{.Portal}}“{{if .Category}} · Kategorie {{.Category}}{{end}}{{if .Tags}} · Schlagwörter {{.Tags}}{{end}} · Priorität {{.Priority}}

{{range .Excerpts}}> **{{.Title}}**{{if .Type}} ({{.Type}}){{end}}
{{if .Body}}> {{.Body}}
{{end}}{{if .Build}}> Build {{.Build}}
{{end}}
{{end}}[Thema in Phoedox öffnen]({{.Link}}){{if .PublicLink}} · [öffentliche Seite]({{.PublicLink}}){{end}}

_Aus Phoedox weitergeleitet. Neue Meldungen kommen hier als Kommentar dazu; wird dieses Issue geschlossen, gilt das Thema dort als erledigt. Wer gemeldet hat, bleibt privat._
