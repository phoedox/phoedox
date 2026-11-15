// Package aichat is the AI chat in projects (ADR-0066): a person works out
// concepts with the model, lets it write into the open page's draft and turns
// a concept into cards they confirm.
//
// The model acts as an AI client of the person (core.Actor with Via "mcp"),
// so every rule for AI clients applies: projects closed to AI, read-only and
// suggestion modes, no admin actions. Each model call is checked against the
// switches and budgets of instance, organization and person first, and its
// cost goes into the usage ledger.
package aichat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/mcp"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// mcpTools are the MCP tools the chat offers: reading, and writes that
// follow the project's AI write mode. Cards go through propose_cards, page
// changes through edit_page.
var mcpTools = []string{
	"search", "get_project", "list_documents", "get_document", "list_document_versions",
	"list_decisions", "create_decision", "update_decision", "create_document",
	"get_entity", "list_entities", "query_entities", "traverse", "software_tree", "diagnose",
	"get_data_model", "upsert_entity", "link_entities",
	"list_tasks", "list_comments", "add_comment", "list_skills", "get_skill",
	"change_summary", "find_documentation_gaps", "list_maintenance_plans",
	"household_overview", "list_locations", "ip_addresses", "list_checks", "get_readings",
}

// maxSteps limits model calls for one message of the person.
const maxSteps = 12

// maxMessages limits the length of a chat; then a new chat starts.
const maxMessages = 400

// Service runs chats.
type Service struct {
	Core  *core.Service
	tools map[string]tool
	list  []ai.Tool
}

type tool struct {
	def  ai.Tool
	call func(ctx context.Context, a core.Actor, input json.RawMessage) (any, error)
}

// New returns the chat service.
func New(c *core.Service) *Service {
	s := &Service{Core: c, tools: map[string]tool{}}
	for _, t := range mcp.ChatTools(c, mcpTools) {
		s.add(ai.Tool{Name: t.Name, Description: t.Description, Schema: t.Schema}, t.Call)
	}
	s.add(editPageTool, s.editPage)
	s.add(proposeCardsTool, s.proposeCards)
	return s
}

func (s *Service) add(def ai.Tool, call func(context.Context, core.Actor, json.RawMessage) (any, error)) {
	s.tools[def.Name] = tool{def: def, call: call}
	s.list = append(s.list, def)
}

// aiActor is the person acting through the chat: an AI client.
func aiActor(a core.Actor, chatID string) core.Actor {
	return core.Actor{User: a.User, Via: "mcp", TokenID: "chat:" + chatID}
}

// View is a chat with its messages prepared for display.
type View struct {
	store.AIChat
	Messages  []MessageView               `json:"messages"`
	Proposals map[string]store.AIProposal `json:"proposals"`
	Status    core.ChatStatus             `json:"status"`
}

// MessageView is one message for display.
type MessageView struct {
	Seq       int        `json:"seq"`
	Role      string     `json:"role"`
	Parts     []ai.Part  `json:"parts"`
	CreatedAt store.Time `json:"createdAt"`
}

func (s *Service) chat(ctx context.Context, a core.Actor, ref, id string) (store.Project, store.AIChat, error) {
	p, err := s.Core.Project(ctx, a, ref)
	if err != nil {
		return p, store.AIChat{}, err
	}
	c, err := s.Core.DB.GetAIChat(ctx, a.User.ID, id)
	if err != nil {
		return p, c, err
	}
	if c.ProjectID != p.ID {
		return p, c, store.ErrNotFound
	}
	return p, c, nil
}

// Status tells whether the person can chat in the project.
func (s *Service) Status(ctx context.Context, a core.Actor, ref string) (core.ChatStatus, error) {
	return s.Core.ChatStatus(ctx, a, ref)
}

// Chats lists the person's chats in a project.
func (s *Service) Chats(ctx context.Context, a core.Actor, ref string) ([]store.AIChat, error) {
	cs, err := s.Core.ChatStatus(ctx, a, ref)
	if err != nil {
		return nil, err
	}
	return s.Core.DB.AIChats(ctx, a.User.ID, cs.ProjectID, 50)
}

// Create starts a chat, optionally about a page.
func (s *Service) Create(ctx context.Context, a core.Actor, ref, docRef string) (store.AIChat, error) {
	cs, err := s.Core.ChatStatus(ctx, a, ref)
	if err != nil {
		return store.AIChat{}, err
	}
	if !cs.Available {
		return store.AIChat{}, core.Invalid(cs.Reason, nil, "the AI chat is not available")
	}
	now := store.Now()
	c := store.AIChat{ID: string(graph.NewID()), ProjectID: cs.ProjectID, UserID: a.User.ID, CreatedAt: now, UpdatedAt: now}
	if docRef != "" {
		doc, err := s.Core.Document(ctx, a, ref, docRef)
		if err != nil {
			return c, err
		}
		c.DocumentID = doc.ID
	}
	if err := s.Core.DB.CreateAIChat(ctx, c); err != nil {
		return c, err
	}
	return c, nil
}

// Get returns a chat with its messages.
func (s *Service) Get(ctx context.Context, a core.Actor, ref, id string) (View, error) {
	_, c, err := s.chat(ctx, a, ref, id)
	if err != nil {
		return View{}, err
	}
	msgs, err := s.Core.DB.AIChatMessages(ctx, c.ID)
	if err != nil {
		return View{}, err
	}
	props, err := s.Core.DB.AIProposals(ctx, c.ID)
	if err != nil {
		return View{}, err
	}
	st, _ := s.Core.ChatStatus(ctx, a, ref)
	v := View{AIChat: c, Messages: []MessageView{}, Proposals: props, Status: st}
	for _, m := range msgs {
		v.Messages = append(v.Messages, MessageView{Seq: m.Seq, Role: m.Role, Parts: ai.Parts(m.Content), CreatedAt: m.CreatedAt})
	}
	return v, nil
}

// Delete removes a chat (the usage ledger keeps its costs).
func (s *Service) Delete(ctx context.Context, a core.Actor, ref, id string) error {
	_, c, err := s.chat(ctx, a, ref, id)
	if err != nil {
		return err
	}
	return s.Core.DB.DeleteAIChat(ctx, a.User.ID, c.ID)
}

// Event is sent to the person while the model works.
type Event struct {
	Type string `json:"type"` // text | tool | tool_done | proposal | notice | done
	Text string `json:"text,omitempty"`
	Tool string `json:"tool,omitempty"`
	OK   bool   `json:"ok,omitempty"`
	// Key is an i18n key (notice).
	Key string `json:"key,omitempty"`
	// Cost of this message in cents (done).
	CostCents int64 `json:"costCents,omitempty"`
}

// Input is a message of the person.
type Input struct {
	Text string `json:"text"`
	// Document is the page the person has open (slug or id), optional.
	Document string `json:"document,omitempty"`
	// Language is the person's interface language (de, en …).
	Language string `json:"language,omitempty"`
	// Model is the model ref to use ("" = the chat's last model or the default).
	Model string `json:"model,omitempty"`
}

// Send adds a message of the person and lets the model answer, calling
// tools as it asks, until it is done (or a limit is reached).
func (s *Service) Send(ctx context.Context, a core.Actor, ref, id string, in Input, emit func(Event)) error {
	p, c, err := s.chat(ctx, a, ref, id)
	if err != nil {
		return err
	}
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" {
		return core.Invalid("validation.ai_chat_empty", nil, "the message is empty")
	}
	if len(in.Text) > 100_000 {
		return core.Invalid("validation.ai_chat_too_long", nil, "the message is too long")
	}
	cs, err := s.Core.ChatStatus(ctx, a, ref)
	if err != nil {
		return err
	}
	if !cs.Available {
		return core.Invalid(cs.Reason, nil, "the AI chat is not available")
	}
	want := in.Model
	if want == "" {
		want = c.Model
	}
	model, prov, err := s.Core.ResolveChatModel(ctx, a, ref, want)
	if err != nil {
		return err
	}
	if !model.Own && cs.InstanceBlocked {
		return core.Invalid("ai_chat.unavailable.instance_models", nil, "the instance's models are used up this month")
	}
	if model.Ref != c.Model {
		_ = s.Core.DB.SetAIChatModel(ctx, c.ID, model.Ref)
	}
	msgs, err := s.Core.DB.AIChatMessages(ctx, c.ID)
	if err != nil {
		return err
	}
	if len(msgs) >= maxMessages {
		return core.Invalid("validation.ai_chat_full", nil, "this chat is full; start a new one")
	}
	history := make([]ai.Message, 0, len(msgs)+2)
	for _, m := range msgs {
		history = append(history, ai.Message{Raw: m.Content, Model: m.Model})
	}
	// Calls left open (an aborted turn or a limit) are answered first.
	var open []ai.ToolResult
	if n := len(msgs); n > 0 && msgs[n-1].Role == "assistant" {
		for _, call := range ai.OpenCalls(msgs[n-1].Content) {
			open = append(open, ai.ToolResult{ID: call.ID, Content: "Not run: the turn ended before this call ran.", IsError: true})
		}
	}
	text := in.Text
	if notes := s.proposalNotes(ctx, c.ID, msgs); notes != "" {
		text = notes + "\n\n" + text
	}
	user := ai.UserMessage(text, open)
	if _, err := s.Core.DB.AppendAIChatMessage(ctx, c.ID, "user", "", user); err != nil {
		return err
	}
	history = append(history, ai.Message{Raw: user})
	if c.Title == "" {
		_ = s.Core.DB.RenameAIChat(ctx, c.ID, core.ChatTitle(in.Text))
	}

	system := []string{resources.Prompt("chat-system"), s.context(ctx, a, p, c, in)}
	actor := aiActor(a, c.ID)
	var spent int64
	defer func() { emit(Event{Type: "done", CostCents: (spent + 9_999) / 10_000}) }()
	for step := 0; ; step++ {
		if step > 0 {
			if cs, err = s.Core.ChatStatus(ctx, a, ref); err != nil {
				return err
			}
			if !cs.Available {
				emit(Event{Type: "notice", Key: cs.Reason})
				return nil
			}
			if !model.Own && cs.InstanceBlocked {
				emit(Event{Type: "notice", Key: "ai_chat.unavailable.instance_models"})
				return nil
			}
		}
		turn, err := prov.Stream(ctx, ai.Request{Model: model.Model, System: system, History: normalize(history), Tools: s.list}, func(e ai.Event) {
			emit(Event{Type: e.Type, Text: e.Text, Tool: e.Tool})
		})
		if err != nil {
			return providerError(err)
		}
		if turn.Usage.Model == "" {
			turn.Usage.Model = model.Model
		}
		spent += s.Core.RecordAIUsage(ctx, a, cs, c.ID, model, turn.Usage)
		if _, err := s.Core.DB.AppendAIChatMessage(ctx, c.ID, "assistant", model.Model, turn.Message); err != nil {
			return err
		}
		history = append(history, ai.Message{Raw: turn.Message, Model: model.Model})
		switch {
		case turn.Stop == ai.StopRefusal:
			emit(Event{Type: "notice", Key: "ai_chat.notice.refused"})
			return nil
		case turn.Stop == ai.StopMaxTokens:
			// Tool inputs may be cut off: do not run them (they are answered
			// as not run with the next message).
			emit(Event{Type: "notice", Key: "ai_chat.notice.cut_off"})
			return nil
		case len(turn.Calls) == 0:
			return nil
		case step == maxSteps-1:
			emit(Event{Type: "notice", Key: "ai_chat.notice.step_limit"})
			return nil
		}
		results := make([]ai.ToolResult, 0, len(turn.Calls))
		for _, call := range turn.Calls {
			res := s.run(ctx, actor, call)
			results = append(results, res)
			emit(Event{Type: "tool_done", Tool: call.Name, OK: !res.IsError})
			if call.Name == proposeCardsTool.Name && !res.IsError {
				emit(Event{Type: "proposal"})
			}
		}
		msg := ai.UserMessage("", results)
		if _, err := s.Core.DB.AppendAIChatMessage(ctx, c.ID, "user", "", msg); err != nil {
			return err
		}
		history = append(history, ai.Message{Raw: msg})
	}
}

// run calls one tool; failures go back to the model as error results.
func (s *Service) run(ctx context.Context, a core.Actor, call ai.ToolCall) ai.ToolResult {
	t, ok := s.tools[call.Name]
	if !ok {
		return ai.ToolResult{ID: call.ID, Content: "unknown tool " + call.Name, IsError: true}
	}
	v, err := t.call(ctx, a, call.Input)
	if err != nil {
		return ai.ToolResult{ID: call.ID, Content: errorText(err), IsError: true}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ai.ToolResult{ID: call.ID, Content: "result could not be encoded", IsError: true}
	}
	const max = 60_000
	out := string(b)
	if len(out) > max {
		out = out[:max] + "\n… (cut off; ask for less, e.g. one document or a filter)"
	}
	return ai.ToolResult{ID: call.ID, Content: out}
}

func errorText(err error) string {
	var inv *core.InvalidError
	switch {
	case errors.As(err, &inv):
		return "invalid input: " + inv.Msg
	case errors.Is(err, core.ErrForbidden):
		return "forbidden: the person lacks the role for this, or the project limits AI access"
	case errors.Is(err, store.ErrNotFound):
		return "not found (or not visible to the person)"
	case errors.Is(err, store.ErrConflict):
		return "conflict: it exists or changed meanwhile; read it again"
	}
	return err.Error()
}

// providerError turns a provider failure into a translated error.
func providerError(err error) error {
	var apiErr *ai.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Status == 401 || apiErr.Status == 403:
			return core.Invalid("ai_chat.error.key", nil, "the AI provider refused the API key")
		case apiErr.Status == 429 || apiErr.Status == 529:
			return core.Invalid("ai_chat.error.busy", nil, "the AI provider is busy; try again in a minute")
		case apiErr.Status == 400:
			return core.Invalid("ai_chat.error.request", nil, "the AI provider refused the request")
		}
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return core.Invalid("ai_chat.error.provider", nil, "the AI provider could not be reached")
}

// normalize merges consecutive user messages (after an aborted turn) so the
// history alternates; stored messages stay as they are.
func normalize(h []ai.Message) []ai.Message {
	out := make([]ai.Message, 0, len(h))
	var prevUser bool
	for _, m := range h {
		var msg struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(m.Raw, &msg) != nil {
			continue
		}
		if msg.Role == "user" && prevUser {
			merged, err := mergeUser(out[len(out)-1].Raw, msg.Content)
			if err == nil {
				out[len(out)-1].Raw = merged
				continue
			}
		}
		prevUser = msg.Role == "user"
		out = append(out, m)
	}
	return out
}

func mergeUser(a json.RawMessage, content json.RawMessage) (json.RawMessage, error) {
	var first struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(a, &first); err != nil {
		return nil, err
	}
	var more []json.RawMessage
	if err := json.Unmarshal(content, &more); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"role": "user", "content": append(first.Content, more...)})
}

// context describes the situation (person, project, open page, date).
func (s *Service) context(ctx context.Context, a core.Actor, p store.Project, c store.AIChat, in Input) string {
	lang := in.Language
	if a.User.Locale != "" {
		lang = a.User.Locale
	}
	if lang == "" {
		lang = "en"
	}
	page := ""
	docRef := in.Document
	if docRef == "" {
		docRef = c.DocumentID
	}
	if docRef != "" {
		if doc, err := s.Core.Document(ctx, a, p.Slug, docRef); err == nil {
			page = strings.NewReplacer("{{title}}", doc.Title, "{{slug}}", doc.Slug, "{{type}}", doc.Type).Replace(resources.Prompt("chat-page"))
		}
	}
	name := a.User.Name
	if name == "" {
		name = "the person"
	}
	locale := p.MainLocale
	if locale == "" {
		locale = "not set"
	}
	return strings.NewReplacer("{{date}}", time.Now().UTC().Format("2006-01-02"), "{{user}}", name, "{{language}}", lang,
		"{{project_name}}", p.Name, "{{project}}", p.Slug, "{{main_locale}}", locale, "{{write_mode}}", p.AIWriteMode,
		"{{page}}", strings.TrimRight(page, "\n")).Replace(resources.Prompt("chat-context"))
}

// proposalNotes tells the model what the person did with proposals since
// its last message.
func (s *Service) proposalNotes(ctx context.Context, chatID string, msgs []store.AIChatMessage) string {
	props, err := s.Core.DB.AIProposals(ctx, chatID)
	if err != nil || len(props) == 0 {
		return ""
	}
	var since time.Time
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" && len(ai.OpenCalls(msgs[i].Content)) == 0 && hasText(msgs[i].Content) {
			since = msgs[i].CreatedAt.Time
			break
		}
	}
	var lines []string
	for _, p := range props {
		if !p.DecidedAt.After(since) {
			continue
		}
		switch p.Status {
		case "accepted":
			var created []struct {
				Title string `json:"title"`
			}
			_ = json.Unmarshal(p.Result, &created)
			titles := make([]string, 0, len(created))
			for _, t := range created {
				titles = append(titles, "“"+t.Title+"”")
			}
			lines = append(lines, fmt.Sprintf("[Phoedox: the person created %d card(s) from proposal %s: %s]", len(created), p.ToolUseID, strings.Join(titles, ", ")))
		case "rejected":
			lines = append(lines, fmt.Sprintf("[Phoedox: the person rejected proposal %s]", p.ToolUseID))
		}
	}
	return strings.Join(lines, "\n")
}

func hasText(raw json.RawMessage) bool {
	for _, p := range ai.Parts(raw) {
		if p.Type == "text" {
			return true
		}
	}
	return false
}
