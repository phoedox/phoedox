Subject: {{if eq .Kind "comment.mention"}}{{.Actor}} mentioned you{{else if eq .Kind "comment.reply"}}New reply from {{.Actor}}{{else if eq .Kind "check.fail"}}Check failing{{else if eq .Kind "check.recover"}}Check back to normal{{else if eq .Kind "task.assigned"}}{{.Actor}} assigned a task to you{{else if eq .Kind "project.summary"}}Weekly summary{{else if eq .Kind "draft.review"}}{{.Actor}} asks for your approval{{else if eq .Kind "draft.changes_requested"}}{{.Actor}} asks for changes{{else if eq .Kind "draft.approved"}}{{.Actor}} approved your changes{{else if eq .Kind "page.review_due"}}Review due{{else if eq .Kind "page.required_reading"}}Please read{{else if eq .Kind "entity.owner"}}{{.Actor}} made you responsible{{else if eq .Kind "entity.review_due"}}Entry review due{{else if eq .Kind "decision.answered"}}{{.Actor}} answered an open decision{{else if eq .Kind "budget.request"}}{{.Actor}} asks for more AI usage{{else if eq .Kind "budget.request_decided"}}Answer to your AI usage request{{else if eq .Kind "bot.spike"}}Many requests blocked{{else if eq .Kind "feedback.topic"}}New feedback topic{{else if eq .Kind "feedback.update"}}News on your feedback{{else if eq .Kind "ai_credits.granted"}}AI credits for you{{else if eq .Kind "feedback.comment"}}New comment to moderate{{else if eq .Kind "budget.warning"}}Budget warning{{else if eq .Kind "entity.warranty"}}Warranty ends soon{{else}}Test message from Phoedox{{end}}: {{.Title}}

Hello {{.Name}},

{{if eq .Kind "task.assigned"}}{{.Actor}} assigned the task "{{.Title}}" to you in project {{.Project}}.{{else if eq .Kind "test"}}this is what notifications from Phoedox look like. You triggered this message yourself.{{else}}{{.Title}} ({{.Project}}){{end}}
{{if .Body}}
{{.Body}}
{{end}}
{{.Link}}

You find the settings for notifications in your profile.
