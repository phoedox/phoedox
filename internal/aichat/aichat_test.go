package aichat

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// fake answers with prepared turns and records the requests.
type fake struct {
	turns []ai.Turn
	reqs  []ai.Request
}

func (f *fake) Model() string { return "claude-opus-5-5" }

func (f *fake) Stream(ctx context.Context, req ai.Request, emit func(ai.Event)) (ai.Turn, error) {
	f.reqs = append(f.reqs, req)
	if len(f.turns) == 0 {
		return ai.Turn{}, errors.New("no more turns")
	}
	t := f.turns[0]
	f.turns = f.turns[1:]
	for _, c := range t.Calls {
		emit(ai.Event{Type: "tool", Tool: c.Name})
	}
	emit(ai.Event{Type: "text", Text: "…"})
	return t, nil
}

// turn builds an assistant message with text and tool calls.
func turn(text string, calls ...ai.ToolCall) ai.Turn {
	blocks := []map[string]any{}
	if text != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	for _, c := range calls {
		blocks = append(blocks, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": c.Input})
	}
	raw, _ := json.Marshal(map[string]any{"role": "assistant", "content": blocks})
	stop := "end_turn"
	if len(calls) > 0 {
		stop = ai.StopToolUse
	}
	return ai.Turn{Message: raw, Calls: calls, Stop: stop, Usage: ai.Usage{Model: "claude-opus-5-5", Input: 1000, Output: 500}}
}

func call(id, name, input string) ai.ToolCall {
	return ai.ToolCall{ID: id, Name: name, Input: json.RawMessage(input)}
}

func setup(t *testing.T) (*Service, *fake, core.Actor, store.Project) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	u, err := db.CreateUser(ctx, store.User{ID: string(graph.NewID()), Email: "jo@x.de", Name: "Jo"})
	if err != nil {
		t.Fatal(err)
	}
	c := core.New(db)
	f := &fake{}
	c.AI = f
	a := core.Actor{User: u, Via: "web"}
	p, err := c.CreateProject(ctx, a, core.ProjectInput{Name: "Home"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateDocument(ctx, a, p.Slug, core.DocumentInput{Type: "markdown", Title: "Backup concept", Markdown: "# Backup\n\nGoal: nightly backups.\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateDocument(ctx, a, p.Slug, core.DocumentInput{Type: store.DocBoard, Title: "Work", Board: &core.Board{Columns: []core.BoardColumn{{ID: "todo", Name: "To do"}, {ID: "done", Name: "Done", Done: true}}}}); err != nil {
		t.Fatal(err)
	}
	return New(c), f, a, p
}

func TestChatEditsDraftAndProposesCards(t *testing.T) {
	ctx := context.Background()
	s, f, a, p := setup(t)
	chat, err := s.Create(ctx, a, p.Slug, "backup-concept")
	if err != nil {
		t.Fatal(err)
	}
	f.turns = []ai.Turn{
		turn("Let me read it.", call("t1", "get_document", `{"project":"home","document":"backup-concept"}`)),
		turn("", call("t2", "edit_page", `{"project":"home","document":"backup-concept","edits":[{"find":"nightly backups","replace":"nightly backups to the NAS and weekly off-site"}]}`)),
		turn("", call("t3", "propose_cards", `{"project":"home","board":"work","documents":["backup-concept"],"cards":[{"title":"Set up NAS backup"},{"title":"Choose off-site target","priority":2}]}`)),
		turn("Done: I changed the draft and proposed two cards."),
	}
	var events []Event
	if err := s.Send(ctx, a, p.Slug, chat.ID, Input{Text: "Improve the concept and plan the work", Document: "backup-concept"}, func(e Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	if len(f.reqs) != 4 {
		t.Fatalf("model calls: %d", len(f.reqs))
	}
	// The situation names the open page; the history alternates.
	if !strings.Contains(f.reqs[0].System[1], "backup-concept") || !strings.Contains(f.reqs[0].System[1], "Jo") {
		t.Fatalf("context: %s", f.reqs[0].System[1])
	}
	last := f.reqs[3].History
	if len(last) != 7 || !strings.Contains(string(last[6]), `"tool_result"`) {
		t.Fatalf("history: %d %s", len(last), last[len(last)-1])
	}
	for _, e := range events {
		if e.Type == "tool_done" && !e.OK {
			t.Fatalf("tool failed: %+v", events)
		}
	}
	if events[len(events)-1].Type != "done" {
		t.Fatalf("last event: %+v", events[len(events)-1])
	}

	// The edit is in the draft, the published page is unchanged.
	d, err := s.Core.Draft(ctx, a, p.Slug, "backup-concept")
	if err != nil || !strings.Contains(d.Markdown, "weekly off-site") {
		t.Fatalf("draft: %v %q", err, d.Markdown)
	}
	doc, _ := s.Core.Document(ctx, a, p.Slug, "backup-concept")
	if strings.Contains(string(doc.Content), "off-site") {
		t.Fatal("published page must not change")
	}

	// The person creates the second card only.
	ts, err := s.Decide(ctx, a, p.Slug, chat.ID, "t3", true, []int{1})
	if err != nil || len(ts) != 1 || ts[0].Title != "Choose off-site target" || ts[0].Priority != 2 {
		t.Fatalf("decide: %v %+v", err, ts)
	}
	tasks, _ := s.Core.Tasks(ctx, a, p.Slug, "", "", "", false)
	if len(tasks) != 1 || len(tasks[0].Documents) != 1 || tasks[0].Documents[0].Slug != "backup-concept" {
		t.Fatalf("tasks: %+v", tasks)
	}
	if _, err := s.Decide(ctx, a, p.Slug, chat.ID, "t3", true, nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second decision must fail: %v", err)
	}

	// The next message tells the model what the person chose.
	f.turns = []ai.Turn{turn("Noted.")}
	if err := s.Send(ctx, a, p.Slug, chat.ID, Input{Text: "Thanks"}, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	if h := f.reqs[4].History; !strings.Contains(string(h[len(h)-1]), "created 1 card") {
		t.Fatalf("note: %s", h[len(h)-1])
	}

	v, err := s.Get(ctx, a, p.Slug, chat.ID)
	if err != nil || v.Title != "Improve the concept and plan the work" || len(v.Messages) != 10 || v.Proposals["t3"].Status != "accepted" {
		t.Fatalf("view: %v %+v", err, v)
	}
	// Five calls of 1000 input and 500 output tokens at $4/$20 per MTok.
	used, _ := s.Core.DB.AIUsageSince(ctx, "", a.User.ID, monthStartForTest())
	if used != 5*(1000*4+500*20) {
		t.Fatalf("usage: %d", used)
	}
}

func TestChatSwitchesAndBudgets(t *testing.T) {
	ctx := context.Background()
	s, f, a, p := setup(t)
	admin := a
	admin.User.IsAdmin = true

	off := false
	if _, err := s.Core.UpdateSettings(ctx, admin, core.SettingsPatch{AIChat: &core.AIChatPatch{Enabled: &off}}); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Status(ctx, a, p.Slug); st.Available || st.Reason != "ai_chat.unavailable.instance_off" {
		t.Fatalf("instance off: %+v", st)
	}
	if _, err := s.Create(ctx, a, p.Slug, ""); err == nil {
		t.Fatal("no chat while switched off")
	}
	on := true
	budget := 1 // one cent
	if _, err := s.Core.UpdateSettings(ctx, admin, core.SettingsPatch{AIChat: &core.AIChatPatch{Enabled: &on, UserBudget: &budget}}); err != nil {
		t.Fatal(err)
	}
	chat, err := s.Create(ctx, a, p.Slug, "")
	if err != nil {
		t.Fatal(err)
	}
	f.turns = []ai.Turn{turn("Hello")}
	if err := s.Send(ctx, a, p.Slug, chat.ID, Input{Text: "Hi"}, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	// 1000×4 + 500×20 micro-dollars = 1.4 cents: the budget is used up.
	st, _ := s.Status(ctx, a, p.Slug)
	if st.Available || st.Reason != "ai_chat.unavailable.user_budget" || st.UserUsed != 2 {
		t.Fatalf("budget: %+v", st)
	}
	if err := s.Send(ctx, a, p.Slug, chat.ID, Input{Text: "Again"}, func(Event) {}); err == nil {
		t.Fatal("send must fail without budget")
	}

	// Projects closed to AI have no chat; read-only projects refuse edits.
	if _, err := s.Core.UpdateProject(ctx, a, p.Slug, core.ProjectInput{Name: "Home", AIWriteMode: core.AIOff}); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Status(ctx, a, p.Slug); st.Reason != "ai_chat.unavailable.project_off" {
		t.Fatalf("project off: %+v", st)
	}
	if _, err := s.Core.UpdateProject(ctx, a, p.Slug, core.ProjectInput{Name: "Home", AIWriteMode: core.AIRead}); err != nil {
		t.Fatal(err)
	}
	md := "x"
	if _, err := s.Core.AIDraftEdit(ctx, aiActor(a, chat.ID), p.Slug, "backup-concept", &md, nil); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("read-only project: %v", err)
	}
	// Tokens and the web are different: only AI actors use AIDraftEdit.
	if _, err := s.Core.AIDraftEdit(ctx, a, p.Slug, "backup-concept", &md, nil); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("web actor: %v", err)
	}
}

func TestOrgSwitchAndBudgetLimits(t *testing.T) {
	ctx := context.Background()
	s, _, a, _ := setup(t)
	admin := a
	admin.User.IsAdmin = true
	o, err := s.Core.CreateOrg(ctx, admin, core.OrgInput{Name: "Acme", AdminEmail: a.User.Email})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Core.CreateProject(ctx, a, core.ProjectInput{Name: "Acme docs", Org: &o.Slug})
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Status(ctx, a, p.Slug); !st.Available || st.OrgLimit != 2000 || st.UserLimit != 500 {
		t.Fatalf("defaults: %+v", st)
	}
	// Org admins may lower the budget, not raise it above the limit.
	high, low, user := 5000, 300, 1000
	if _, err := s.Core.UpdateOrg(ctx, a, o.Slug, core.OrgInput{AIChatBudget: &high}); err == nil {
		t.Fatal("budget above the limit must fail")
	}
	if _, err := s.Core.UpdateOrg(ctx, a, o.Slug, core.OrgInput{AIChatBudget: &low, AIChatUserBudget: &user}); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Status(ctx, a, p.Slug); st.OrgLimit != 300 || st.UserLimit != 1000 {
		t.Fatalf("org budget: %+v", st)
	}
	// Only system admins set the cap.
	if _, err := s.Core.UpdateOrg(ctx, a, o.Slug, core.OrgInput{AIChatCap: &high}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("cap by org admin: %v", err)
	}
	off := false
	if _, err := s.Core.UpdateOrg(ctx, a, o.Slug, core.OrgInput{AIChat: &off}); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Status(ctx, a, p.Slug); st.Reason != "ai_chat.unavailable.org_off" {
		t.Fatalf("org off: %+v", st)
	}
	info, err := s.Core.OrgAIChatInfo(ctx, a, o.Slug)
	if err != nil || info.Enabled || *info.Budget != 300 || info.Limit != 2000 {
		t.Fatalf("info: %v %+v", err, info)
	}
}

func TestOpenCallsAreAnsweredAndHistoryMerged(t *testing.T) {
	ctx := context.Background()
	s, f, a, p := setup(t)
	chat, _ := s.Create(ctx, a, p.Slug, "")
	// The answer is cut off while calling a tool: the tool is not run.
	cut := turn("", call("x1", "edit_page", `{"project":"home","document":"backup-concept","markdown":"trunc"}`))
	cut.Stop = ai.StopMaxTokens
	f.turns = []ai.Turn{cut}
	var notice string
	if err := s.Send(ctx, a, p.Slug, chat.ID, Input{Text: "Rewrite it"}, func(e Event) {
		if e.Type == "notice" {
			notice = e.Key
		}
	}); err != nil {
		t.Fatal(err)
	}
	if notice != "ai_chat.notice.cut_off" {
		t.Fatalf("notice: %q", notice)
	}
	if _, err := s.Core.Draft(ctx, a, p.Slug, "backup-concept"); err == nil {
		t.Fatal("a cut-off tool call must not run")
	}
	f.turns = []ai.Turn{turn("Sorry.")}
	if err := s.Send(ctx, a, p.Slug, chat.ID, Input{Text: "Shorter please"}, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	last := string(f.reqs[1].History[2])
	if !strings.Contains(last, `"tool_use_id":"x1"`) || !strings.Contains(last, "Shorter please") {
		t.Fatalf("open call not answered: %s", last)
	}

	h := normalize([]json.RawMessage{
		json.RawMessage(`{"role":"user","content":[{"type":"text","text":"a"}]}`),
		json.RawMessage(`{"role":"user","content":[{"type":"text","text":"b"}]}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"c"}]}`),
	})
	if len(h) != 2 || !strings.Contains(string(h[0]), `"b"`) {
		t.Fatalf("normalize: %s", h)
	}
}

func monthStartForTest() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}
