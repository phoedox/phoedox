package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

func TestAutomationRules(t *testing.T) {
	ctx := context.Background()
	s, owner, editor, _ := setup(t)
	s.RequestPolicy = requests.Policy{AllowPrivate: true}
	box, _ := secret.New(make([]byte, 32))
	s.Box = box
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, editor.User.Email, store.RoleEditor)
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Aufgaben"})

	// Owners only; checks of the definition come back as keys.
	if _, err := s.SaveRule(ctx, editor, p.Slug, "", RuleInput{Name: "x", Trigger: RuleTrigger{Type: TriggerCheckFail}, Action: RuleAction{Type: ActionNotify}}); err == nil {
		t.Fatal("editors must not create rules")
	}
	if _, err := s.SaveRule(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug, "", RuleInput{Name: "x", Trigger: RuleTrigger{Type: TriggerCheckFail}, Action: RuleAction{Type: ActionNotify}}); err == nil {
		t.Fatal("AI clients must not create rules")
	}
	if _, err := s.SaveRule(ctx, owner, p.Slug, "", RuleInput{Name: "x", Trigger: RuleTrigger{Type: TriggerDate}, Action: RuleAction{Type: ActionNotify}}); invalidKey(err) != "validation.rule_field" {
		t.Fatalf("date without field: %v", err)
	}

	// Check fails → task on the board.
	if _, err := s.SaveRule(ctx, owner, p.Slug, "", RuleInput{Name: "Ausfall", Trigger: RuleTrigger{Type: TriggerCheckFail},
		Action: RuleAction{Type: ActionTask, Board: board.Document.Slug, Title: "{{check}} ausgefallen: {{message}}", Labels: []string{"monitoring"}}}); err != nil {
		t.Fatal(err)
	}
	s.ruleCheck(ctx, owner, store.Check{ID: "c1", ProjectID: p.ID, Name: "NAS", LastStatus: store.CheckFail, LastMessage: "timeout"}, TriggerCheckFail)
	tasks, _ := s.Tasks(ctx, owner, p.Slug, board.Document.Slug, "", "", false)
	if len(tasks) != 1 || tasks[0].Title != "NAS ausgefallen: timeout" || len(tasks[0].Labels) != 1 {
		t.Fatalf("task: %+v", tasks)
	}
	// A recovery does not match a fail rule.
	s.ruleCheck(ctx, owner, store.Check{ID: "c1", ProjectID: p.ID, Name: "NAS", LastStatus: store.CheckOK}, TriggerCheckRecover)
	if tasks, _ = s.Tasks(ctx, owner, p.Slug, board.Document.Slug, "", "", false); len(tasks) != 1 {
		t.Fatalf("recovery: %d", len(tasks))
	}

	// New contract → nothing; contract ends in 20 days → request to a ticket system with a secret from the environment.
	var got struct {
		auth string
		body map[string]any
	}
	ticket := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got.body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer ticket.Close()
	if err := s.SetVariable(ctx, owner, p.Slug, "TICKET_TOKEN", "geheim", true); err != nil {
		t.Fatal(err)
	}
	rule, err := s.SaveRule(ctx, owner, p.Slug, "", RuleInput{Name: "Kündigen", Trigger: RuleTrigger{Type: TriggerDate, Kind: "contract", Field: "cancel_by", DaysBefore: 30},
		Action: RuleAction{Type: ActionHTTP, Request: "POST " + ticket.URL + "/api/v1/tickets\nAuthorization: Token {{TICKET_TOKEN}}\nContent-Type: application/json\n\n{\"title\": {{entry|json}}, \"note\": {{days|json}}}"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	if _, err := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "contract", Name: "Internet {{TICKET_TOKEN}}", Attrs: map[string]any{"cancel_by": "2026-12-05"}}); err != nil {
		t.Fatal(err)
	}
	s.RunRuleDates(ctx, now)
	if got.auth != "Token geheim" || got.body["title"] != "Internet { {TICKET_TOKEN} }" || got.body["note"] != "20" {
		t.Fatalf("ticket request: %+v", got)
	}
	r, _ := s.DB.GetRule(ctx, p.ID, rule.ID)
	if r.LastStatus != "ok" {
		t.Fatalf("rule result: %+v", r)
	}
	// The same date fires only once, also on the next day.
	got.auth = ""
	s.RunRuleDates(ctx, now.Add(24*time.Hour))
	if got.auth != "" {
		t.Fatal("date fired twice")
	}

	// A failing request is recorded on the rule.
	ticket.Close()
	if err := s.TestRule(ctx, owner, p.Slug, rule.ID); err == nil {
		t.Fatal("request to a closed server succeeded")
	}
	r, _ = s.DB.GetRule(ctx, p.ID, rule.ID)
	if r.LastStatus != "error" || !strings.Contains(r.LastError, "request failed") {
		t.Fatalf("error result: %+v", r)
	}

	// What rules do never triggers rules.
	if _, err := s.SaveRule(ctx, owner, p.Slug, "", RuleInput{Name: "Neues Gerät", Trigger: RuleTrigger{Type: TriggerEntityCreate, Kind: "device"},
		Action: RuleAction{Type: ActionTask, Board: board.Document.Slug, Title: "{{entry}} dokumentieren"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertEntity(ctx, Actor{User: owner.User, Via: "rule", TokenID: "r"}, p.Slug, EntityInput{Kind: "device", Name: "Von Regel"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertEntity(ctx, editor, p.Slug, EntityInput{Kind: "device", Name: "Drucker"}); err != nil {
		t.Fatal(err)
	}
	tasks, _ = s.Tasks(ctx, owner, p.Slug, board.Document.Slug, "", "", false)
	titles := []string{}
	for _, x := range tasks {
		titles = append(titles, x.Title)
	}
	if strings.Join(titles, "|") != "NAS ausgefallen: timeout|Drucker dokumentieren" && strings.Join(titles, "|") != "Drucker dokumentieren|NAS ausgefallen: timeout" {
		t.Fatalf("entry rule: %v", titles)
	}
}
