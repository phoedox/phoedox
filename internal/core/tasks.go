package core

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Task boards (ADR-0030): a board is a document of type "board" holding its
// columns; tasks are its cards with their own table, so moving a card does
// not create a document version.

// Board is the content of a board document.
type Board struct {
	Description string        `json:"description,omitempty"` // Markdown above the board
	Columns     []BoardColumn `json:"columns"`
}

// BoardColumn is a column; tasks in a Done column count as done.
type BoardColumn struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Done bool   `json:"done,omitempty"`
}

var columnIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// defaultColumns are used when a board is created without columns (the web
// app sends columns named in the user's language).
var defaultColumns = []BoardColumn{{ID: "todo", Name: "To do"}, {ID: "doing", Name: "In progress"}, {ID: "done", Name: "Done", Done: true}}

func boardContent(b *Board) (store.JSON, string, error) {
	if b == nil {
		b = &Board{}
	}
	if len(b.Columns) == 0 {
		b.Columns = slices.Clone(defaultColumns)
	}
	if len(b.Columns) > 20 {
		return nil, "", invalid("validation.board_columns", nil, "a board has 1 to 20 columns")
	}
	seen := map[string]bool{}
	for i := range b.Columns {
		c := &b.Columns[i]
		c.Name = strings.TrimSpace(c.Name)
		if c.ID == "" {
			c.ID = strings.Trim(slugish(c.Name), "-")
		}
		if c.Name == "" || !columnIDRe.MatchString(c.ID) || seen[c.ID] {
			return nil, "", invalid("validation.board_column", map[string]any{"column": c.Name}, "column %q needs a name and a unique id", c.Name)
		}
		seen[c.ID] = true
	}
	j, _ := json.Marshal(b)
	return j, b.Description, nil
}

func slugish(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == 'ä':
			b.WriteString("ae")
		case r == 'ö':
			b.WriteString("oe")
		case r == 'ü':
			b.WriteString("ue")
		case r == 'ß':
			b.WriteString("ss")
		default:
			b.WriteRune('-')
		}
	}
	out := b.String()
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

func boardOf(doc store.Document) Board {
	var b Board
	_ = json.Unmarshal(doc.Content, &b)
	return b
}

// column finds a column by id or (case-insensitive) name.
func (b Board) column(ref string) (BoardColumn, bool) {
	for _, c := range b.Columns {
		if c.ID == ref || strings.EqualFold(c.Name, ref) {
			return c, true
		}
	}
	return BoardColumn{}, false
}

// afterBoardChange keeps tasks on existing columns: cards of removed columns
// move to the first column.
func (s *Service) afterBoardChange(ctx context.Context, doc store.Document) {
	b := boardOf(doc)
	if len(b.Columns) == 0 {
		return
	}
	ids := make([]string, len(b.Columns))
	for i, c := range b.Columns {
		ids[i] = c.ID
	}
	_ = s.DB.MoveOrphanTasks(ctx, doc.ID, ids, b.Columns[0].ID, b.Columns[0].Done)
}

// TaskInput creates a task; TaskPatch changes one (nil = unchanged).
type TaskInput struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Column      string   `json:"column,omitempty"`   // id or name; default: first column
	Assignee    string   `json:"assignee,omitempty"` // user id or e-mail of a project member, or free text such as "Claude"
	Due         string   `json:"due,omitempty"`      // YYYY-MM-DD
	Priority    int      `json:"priority,omitempty"` // 0 none … 3 high
	Labels      []string `json:"labels,omitempty"`
}

type TaskPatch struct {
	Title       *string   `json:"title,omitempty"`
	Description *string   `json:"description,omitempty"`
	Column      *string   `json:"column,omitempty"`
	Position    *float64  `json:"position,omitempty"` // within the column; default: end when the column changes
	Assignee    *string   `json:"assignee,omitempty"` // "" removes
	Due         *string   `json:"due,omitempty"`
	Priority    *int      `json:"priority,omitempty"`
	Labels      *[]string `json:"labels,omitempty"`
	Board       *string   `json:"board,omitempty"` // move to another board of the project
}

// boardAccess loads a board document with the needed role.
func (s *Service) boardAccess(ctx context.Context, a Actor, ref, boardRef, need string) (store.Project, store.Document, error) {
	p, doc, err := s.docAccess(ctx, a, ref, boardRef, need)
	if err != nil {
		return p, doc, err
	}
	if doc.Type != store.DocBoard {
		return p, doc, invalid("validation.not_a_board", nil, "this document is not a board")
	}
	if need != store.RoleReader && a.Via == "mcp" && p.AIWriteMode != AIDirect {
		// Tasks have no suggestions; AI clients change them only when allowed to write directly.
		return p, doc, invalid("validation.ai_tasks_direct", nil, "AI clients change tasks only in projects where AI writes directly")
	}
	return p, doc, nil
}

// assignee resolves an assignee: a project member (id or e-mail) becomes a
// user id, anything else is kept as free text (e.g. "Claude", "ChatGPT").
// An e-mail of someone who is not a member stays an error, it is a typo or a
// missing invitation.
func (s *Service) assignee(ctx context.Context, p store.Project, ref string) (id, text string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", nil
	}
	if id, err := s.resolveAssignee(ctx, p, ref); err == nil {
		return id, "", nil
	}
	if strings.Contains(ref, "@") {
		return "", "", invalid("validation.assignee", map[string]any{"user": ref}, "%q is not a member of this project", ref)
	}
	if r := []rune(ref); len(r) > 60 {
		ref = string(r[:60])
	}
	return "", ref, nil
}

func (s *Service) resolveAssignee(ctx context.Context, p store.Project, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	var u store.User
	var err error
	if strings.Contains(ref, "@") {
		u, err = s.DB.UserByEmail(ctx, ref)
	} else {
		u, err = s.DB.UserByID(ctx, ref)
	}
	if err == nil {
		if m, err2 := s.DB.ProjectFor(ctx, p.ID, u.ID); err2 == nil && m.Role != "" && m.Role != store.RoleDoc {
			return u.ID, nil
		}
	}
	return "", invalid("validation.assignee", map[string]any{"user": ref}, "%q is not a member of this project", ref)
}

func validDue(d string) error {
	if d == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", d); err != nil {
		return invalid("validation.due", nil, "due must be YYYY-MM-DD")
	}
	return nil
}

func cleanLabels(in []string) []string {
	out := []string{}
	for _, l := range in {
		l = strings.TrimSpace(strings.ReplaceAll(l, ",", " "))
		if l != "" && !slices.Contains(out, l) && len(out) < 10 {
			out = append(out, l)
		}
	}
	return out
}

// Tasks lists the tasks of a project, one board or one assignee.
func (s *Service) Tasks(ctx context.Context, a Actor, ref string, boardRef, column, assignee string, open bool) ([]store.Task, error) {
	f := store.TaskFilter{Open: open}
	var p store.Project
	var err error
	if boardRef != "" {
		var doc store.Document
		if p, doc, err = s.boardAccess(ctx, a, ref, boardRef, store.RoleReader); err != nil {
			return nil, err
		}
		f.BoardID = doc.ID
		if column != "" {
			if c, ok := boardOf(doc).column(column); ok {
				f.Column = c.ID
			} else {
				return []store.Task{}, nil
			}
		}
	} else if p, err = s.project(ctx, a, ref, store.RoleDoc); err != nil {
		return nil, err
	}
	if assignee == "me" {
		f.AssigneeID = a.User.ID
	} else if assignee != "" {
		var text string
		if f.AssigneeID, text, err = s.assignee(ctx, p, assignee); err == nil && text != "" {
			f.AssigneeText = text
		}
		if err != nil {
			return nil, err
		}
	}
	list, err := s.DB.ListTasks(ctx, p.ID, f)
	if err != nil {
		return nil, err
	}
	see, err := s.docFilter(ctx, a, p)
	if err != nil {
		return nil, err
	}
	out := list[:0]
	for _, t := range list {
		if see(t.BoardID, false) || boardRef != "" {
			out = append(out, t)
		}
	}
	return out, nil
}

// CreateTask adds a card to a board (at the end of its column).
func (s *Service) CreateTask(ctx context.Context, a Actor, ref, boardRef string, in TaskInput) (store.Task, error) {
	p, doc, err := s.boardAccess(ctx, a, ref, boardRef, store.RoleEditor)
	if err != nil {
		return store.Task{}, err
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return store.Task{}, invalid("validation.title_required", nil, "title is required")
	}
	b := boardOf(doc)
	col := b.Columns[0]
	if in.Column != "" {
		var ok bool
		if col, ok = b.column(in.Column); !ok {
			return store.Task{}, invalid("validation.board_column_unknown", map[string]any{"column": in.Column}, "unknown column %q", in.Column)
		}
	}
	if err := validDue(in.Due); err != nil {
		return store.Task{}, err
	}
	assignee, assigneeText, err := s.assignee(ctx, p, in.Assignee)
	if err != nil {
		return store.Task{}, err
	}
	pos, err := s.DB.MaxTaskPosition(ctx, doc.ID, col.ID)
	if err != nil {
		return store.Task{}, err
	}
	now := store.Now()
	t := store.Task{ID: newID(), ProjectID: p.ID, BoardID: doc.ID, Column: col.ID, Position: pos + 1, Title: in.Title, Description: in.Description,
		AssigneeID: assignee, AssigneeText: assigneeText, Due: in.Due, Priority: min(max(in.Priority, 0), 3), Labels: cleanLabels(in.Labels), CreatedBy: a.String(), CreatedAt: now, UpdatedAt: now}
	if col.Done {
		t.DoneAt = now
	}
	if err := s.DB.SaveTask(ctx, t); err != nil {
		return t, err
	}
	s.event(ctx, a, "task.create", t.ID, map[string]any{"board": doc.ID, "title": t.Title, "column": t.Column})
	return s.DB.GetTask(ctx, p.ID, t.ID)
}

// UpdateTask changes or moves a card.
func (s *Service) UpdateTask(ctx context.Context, a Actor, ref, id string, patch TaskPatch) (store.Task, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return store.Task{}, err
	}
	t, err := s.DB.GetTask(ctx, p.ID, id)
	if err != nil {
		return t, err
	}
	boardRef := t.BoardID
	if patch.Board != nil {
		boardRef = *patch.Board
	}
	_, doc, err := s.boardAccess(ctx, a, ref, boardRef, store.RoleEditor)
	if err != nil {
		return t, err
	}
	if doc.ID != t.BoardID { // moving away also needs edit rights on the old board
		if _, _, err := s.boardAccess(ctx, a, ref, t.BoardID, store.RoleEditor); err != nil {
			return t, err
		}
	}
	b := boardOf(doc)
	moved := doc.ID != t.BoardID
	t.BoardID = doc.ID
	if patch.Title != nil {
		if strings.TrimSpace(*patch.Title) == "" {
			return t, invalid("validation.title_required", nil, "title is required")
		}
		t.Title = strings.TrimSpace(*patch.Title)
	}
	if patch.Description != nil {
		t.Description = *patch.Description
	}
	col, ok := b.column(t.Column)
	if patch.Column != nil {
		if col, ok = b.column(*patch.Column); !ok {
			return t, invalid("validation.board_column_unknown", map[string]any{"column": *patch.Column}, "unknown column %q", *patch.Column)
		}
		moved = moved || col.ID != t.Column
	} else if !ok {
		col, moved = b.Columns[0], true
	}
	t.Column = col.ID
	if patch.Position != nil {
		t.Position = *patch.Position
	} else if moved {
		pos, err := s.DB.MaxTaskPosition(ctx, doc.ID, col.ID)
		if err != nil {
			return t, err
		}
		t.Position = pos + 1
	}
	if col.Done && t.DoneAt.IsZero() {
		t.DoneAt = store.Now()
	} else if !col.Done {
		t.DoneAt = store.Time{}
	}
	if patch.Assignee != nil {
		if t.AssigneeID, t.AssigneeText, err = s.assignee(ctx, p, *patch.Assignee); err != nil {
			return t, err
		}
	}
	if patch.Due != nil {
		if err := validDue(*patch.Due); err != nil {
			return t, err
		}
		t.Due = *patch.Due
	}
	if patch.Priority != nil {
		t.Priority = min(max(*patch.Priority, 0), 3)
	}
	if patch.Labels != nil {
		t.Labels = cleanLabels(*patch.Labels)
	}
	t.UpdatedAt = store.Now()
	if err := s.DB.SaveTask(ctx, t); err != nil {
		return t, err
	}
	s.event(ctx, a, "task.update", t.ID, map[string]any{"board": t.BoardID, "column": t.Column, "patch": patch})
	return s.DB.GetTask(ctx, p.ID, t.ID)
}

// DeleteTask removes a card.
func (s *Service) DeleteTask(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return err
	}
	t, err := s.DB.GetTask(ctx, p.ID, id)
	if err != nil {
		return err
	}
	if _, _, err := s.boardAccess(ctx, a, ref, t.BoardID, store.RoleEditor); err != nil {
		return err
	}
	if err := s.DB.DeleteTask(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "task.delete", id, map[string]any{"board": t.BoardID, "title": t.Title})
	return nil
}
