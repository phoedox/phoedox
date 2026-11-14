package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// Queries over the knowledge graph (ADR-0044): one kind, conditions on its
// fields and on its relations. The table view builds them, pages show them
// in ```query blocks and AI clients run them by name.

// GraphQuery selects entries.
type GraphQuery struct {
	Kind    string    `json:"kind"`
	Where   []Cond    `json:"where,omitempty"`
	Rels    []RelCond `json:"rels,omitempty"`
	Columns []string  `json:"columns,omitempty"` // fields to show; empty = all fields of the kind
	Sort    string    `json:"sort,omitempty"`    // field, "-field" descending; default name
	Limit   int       `json:"limit,omitempty"`   // default 500, at most 2000
}

// Cond compares a field: eq, ne, contains, empty, set, gt, lt. Fields are
// attribute names, "name" or "slug".
type Cond struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value,omitempty"`
}

// RelCond requires (or, with Missing, forbids) an edge rel in the given
// direction, optionally to a given entry or an entry of a given kind.
type RelCond struct {
	Rel     string `json:"rel"`
	In      bool   `json:"in,omitempty"`   // the edge points at the entry
	To      string `json:"to,omitempty"`   // slug of the other entry
	Kind    string `json:"kind,omitempty"` // kind of the other entry
	Missing bool   `json:"missing,omitempty"`
}

var condOps = []string{"eq", "ne", "contains", "empty", "set", "gt", "lt"}

// QueryRow is one entry of a result.
type QueryRow struct {
	Slug  string         `json:"slug"`
	Name  string         `json:"name"`
	Kind  string         `json:"kind"`
	Attrs map[string]any `json:"attrs"`
}

// QueryResult is the answer of a query.
type QueryResult struct {
	Kind    string     `json:"kind"`
	Columns []string   `json:"columns"`
	Rows    []QueryRow `json:"rows"`
	Total   int        `json:"total"` // matches before the limit
}

func (q *GraphQuery) normalize(m Model) error {
	q.Kind = strings.TrimSpace(q.Kind)
	k, ok := m.Find(q.Kind)
	if !ok {
		return invalid("validation.kind_unknown", map[string]any{"kind": q.Kind}, "unknown kind %q", q.Kind)
	}
	if len(q.Where) > 20 || len(q.Rels) > 10 {
		return invalid("validation.query_size", nil, "at most 20 conditions and 10 relation conditions")
	}
	known := func(f string) bool {
		return f == "name" || f == "slug" || slices.ContainsFunc(k.Attrs, func(a Attr) bool { return a.Name == f })
	}
	for _, c := range q.Where {
		if !slices.Contains(condOps, c.Op) {
			return invalid("validation.query_op", map[string]any{"op": c.Op}, "unknown operator %q", c.Op)
		}
		if !known(c.Field) {
			return invalid("validation.rule_field", map[string]any{"field": c.Field}, "the kind has no field %q", c.Field)
		}
	}
	for _, r := range q.Rels {
		if !validRel(r.Rel) {
			return invalid("validation.rel", nil, "rel must be lowercase letters, digits and _, e.g. located_in")
		}
	}
	cols := []string{}
	for _, c := range q.Columns {
		if known(c) && c != "name" && !slices.Contains(cols, c) {
			cols = append(cols, c)
		}
	}
	if len(cols) == 0 {
		for _, a := range k.Attrs {
			cols = append(cols, a.Name)
		}
	}
	q.Columns = cols
	if q.Limit <= 0 || q.Limit > 2000 {
		q.Limit = min(max(q.Limit, 500), 2000)
		if q.Limit <= 0 {
			q.Limit = 500
		}
	}
	return nil
}

func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

// matches checks one condition against a value (case-insensitive text).
func (c Cond) matches(v any) bool {
	got, want := strings.ToLower(text(v)), strings.ToLower(text(c.Value))
	switch c.Op {
	case "empty":
		return got == ""
	case "set":
		return got != ""
	case "eq":
		if a, ok := number(v); ok {
			if b, ok := number(c.Value); ok {
				return a == b
			}
		}
		return got == want
	case "ne":
		return got != want
	case "contains":
		return strings.Contains(got, want)
	case "gt", "lt":
		a, ok1 := number(v)
		b, ok2 := number(c.Value)
		if !ok1 || !ok2 {
			if c.Op == "gt" {
				return got > want // dates and versions as text
			}
			return got != "" && got < want
		}
		if c.Op == "gt" {
			return a > b
		}
		return a < b
	}
	return false
}

// RunQuery runs a query (readers).
func (s *Service) RunQuery(ctx context.Context, a Actor, ref string, q GraphQuery) (QueryResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return QueryResult{}, err
	}
	return s.runQuery(ctx, p, q)
}

func (s *Service) runQuery(ctx context.Context, p store.Project, q GraphQuery) (QueryResult, error) {
	m, err := s.model(ctx, p.ID)
	if err != nil {
		return QueryResult{}, err
	}
	if err := q.normalize(m); err != nil {
		return QueryResult{}, err
	}
	ents, err := s.DB.ListEntities(ctx, p.ID, q.Kind)
	if err != nil {
		return QueryResult{}, err
	}
	var edges []store.Edge
	slugOf := map[string]string{}
	kindOf := map[string]string{}
	if len(q.Rels) > 0 {
		if edges, err = s.DB.EdgesOfProject(ctx, p.ID); err != nil {
			return QueryResult{}, err
		}
		all, err := s.DB.ListEntities(ctx, p.ID, "")
		if err != nil {
			return QueryResult{}, err
		}
		for _, e := range all {
			slugOf[e.ID], kindOf[e.ID] = e.Slug, e.Kind
		}
	}
	hasRel := func(id string, r RelCond) bool {
		for _, e := range edges {
			self, other := e.From, e.To
			if r.In {
				self, other = e.To, e.From
			}
			if self != id || e.Rel != r.Rel {
				continue
			}
			if (r.To == "" || slugOf[other] == r.To) && (r.Kind == "" || kindOf[other] == r.Kind) {
				return true
			}
		}
		return false
	}
	res := QueryResult{Kind: q.Kind, Columns: q.Columns, Rows: []QueryRow{}}
	for _, e := range ents {
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		value := func(f string) any {
			switch f {
			case "name":
				return e.Name
			case "slug":
				return e.Slug
			}
			return attrs[f]
		}
		ok := true
		for _, c := range q.Where {
			if !c.matches(value(c.Field)) {
				ok = false
				break
			}
		}
		for _, r := range q.Rels {
			if ok && hasRel(e.ID, r) == r.Missing {
				ok = false
			}
		}
		if ok {
			res.Rows = append(res.Rows, QueryRow{Slug: e.Slug, Name: e.Name, Kind: e.Kind, Attrs: attrs})
		}
	}
	field, desc := strings.TrimPrefix(q.Sort, "-"), strings.HasPrefix(q.Sort, "-")
	sort.SliceStable(res.Rows, func(i, j int) bool {
		a, b := res.Rows[i], res.Rows[j]
		var x, y any = a.Name, b.Name
		if field != "" && field != "name" {
			x, y = a.Attrs[field], b.Attrs[field]
		}
		less := false
		if fx, ok := number(x); ok {
			if fy, ok := number(y); ok {
				less = fx < fy
			} else {
				less = true
			}
		} else {
			less = strings.ToLower(text(x)) < strings.ToLower(text(y))
		}
		if desc {
			return !less && text(x) != text(y)
		}
		return less
	})
	res.Total = len(res.Rows)
	if len(res.Rows) > q.Limit {
		res.Rows = res.Rows[:q.Limit]
	}
	return res, nil
}

// SavedQueryInfo is a saved query with its parsed definition.
type SavedQueryInfo struct {
	store.SavedQuery
	Def GraphQuery `json:"def"`
}

func savedInfo(q store.SavedQuery) SavedQueryInfo {
	info := SavedQueryInfo{SavedQuery: q}
	_ = json.Unmarshal(q.Query, &info.Def)
	return info
}

// SavedQueries lists the saved queries of a project (readers).
func (s *Service) SavedQueries(ctx context.Context, a Actor, ref string) ([]SavedQueryInfo, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	list, err := s.DB.SavedQueries(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := make([]SavedQueryInfo, 0, len(list))
	for _, q := range list {
		out = append(out, savedInfo(q))
	}
	return out, nil
}

// SaveQueryInput creates or changes a saved query.
type SaveQueryInput struct {
	Name  string     `json:"name"`
	Query GraphQuery `json:"query"`
}

// SaveQuery stores a query under a name (editors); id "" creates one.
func (s *Service) SaveQuery(ctx context.Context, a Actor, ref, id string, in SaveQueryInput) (SavedQueryInfo, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return SavedQueryInfo{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 {
		return SavedQueryInfo{}, invalid("validation.title_required", nil, "name is required (at most 80 characters)")
	}
	m, err := s.model(ctx, p.ID)
	if err != nil {
		return SavedQueryInfo{}, err
	}
	if err := in.Query.normalize(m); err != nil {
		return SavedQueryInfo{}, err
	}
	raw, _ := json.Marshal(in.Query)
	now := store.Now()
	q := store.SavedQuery{ID: newID(), ProjectID: p.ID, CreatedBy: a.String(), CreatedAt: now}
	if id != "" {
		if q, err = s.DB.GetSavedQuery(ctx, p.ID, id); err != nil {
			return SavedQueryInfo{}, err
		}
	}
	q.Name, q.Slug, q.Query, q.UpdatedAt = in.Name, graph.Slugify(in.Name), raw, now
	if q.Slug == "" {
		q.Slug = "query"
	}
	if err := s.DB.SaveQuery(ctx, q); errors.Is(err, store.ErrConflict) {
		return SavedQueryInfo{}, invalid("validation.query_name_taken", map[string]any{"name": in.Name}, "a query named %q exists", in.Name)
	} else if err != nil {
		return SavedQueryInfo{}, err
	}
	s.event(ctx, a, "query.save", q.ID, map[string]string{"name": q.Name})
	return savedInfo(q), nil
}

// DeleteQuery removes a saved query (editors).
func (s *Service) DeleteQuery(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return err
	}
	q, err := s.DB.GetSavedQuery(ctx, p.ID, id)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteSavedQuery(ctx, p.ID, q.ID); err != nil {
		return err
	}
	s.event(ctx, a, "query.delete", q.ID, map[string]string{"name": q.Name})
	return nil
}

// RunSavedQuery runs a saved query by slug or id (readers).
func (s *Service) RunSavedQuery(ctx context.Context, a Actor, ref, queryRef string) (QueryResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return QueryResult{}, err
	}
	q, err := s.DB.GetSavedQuery(ctx, p.ID, strings.TrimSpace(queryRef))
	if err != nil {
		return QueryResult{}, err
	}
	return s.runQuery(ctx, p, savedInfo(q).Def)
}

// BulkInput changes many entries of a project at once (table view, CSV import).
type BulkInput struct {
	Upserts []EntityInput `json:"upserts,omitempty"`
	// Set merges Attrs into the entries Slugs (bulk edit); a null value removes a field.
	Set *struct {
		Slugs []string       `json:"slugs"`
		Attrs map[string]any `json:"attrs"`
	} `json:"set,omitempty"`
	Delete []string `json:"delete,omitempty"`
}

// BulkError is a failed row.
type BulkError struct {
	Row    int            `json:"row"` // index in upserts, set.slugs or delete
	Ref    string         `json:"ref,omitempty"`
	Key    string         `json:"key"`
	Params map[string]any `json:"params,omitempty"`
}

// BulkResult tells what a bulk change did.
type BulkResult struct {
	Created     int         `json:"created"`
	Updated     int         `json:"updated"`
	Deleted     int         `json:"deleted"`
	Suggestions int         `json:"suggestions"`
	Errors      []BulkError `json:"errors"`
}

const maxBulk = 2000

// Bulk applies upserts, a field change and deletions row by row (editors);
// a bad row is reported and the others go on.
func (s *Service) Bulk(ctx context.Context, a Actor, ref string, in BulkInput) (BulkResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return BulkResult{}, err
	}
	n := len(in.Upserts) + len(in.Delete)
	if in.Set != nil {
		n += len(in.Set.Slugs)
	}
	if n > maxBulk {
		return BulkResult{}, invalid("validation.bulk_size", map[string]any{"max": maxBulk}, "at most %d rows at once", maxBulk)
	}
	res := BulkResult{Errors: []BulkError{}}
	fail := func(row int, refName string, err error) {
		be := BulkError{Row: row, Ref: refName, Key: "error.unknown"}
		var ie *InvalidError
		switch {
		case errors.As(err, &ie):
			be.Key, be.Params = ie.Key, ie.Params
		case errors.Is(err, store.ErrNotFound):
			be.Key, be.Params = "validation.entity_not_found", map[string]any{"entity": refName}
		}
		res.Errors = append(res.Errors, be)
	}
	count := func(r EntityResult) {
		switch {
		case r.Suggestion != nil:
			res.Suggestions++
		case r.Created:
			res.Created++
		default:
			res.Updated++
		}
	}
	for i, u := range in.Upserts {
		r, err := s.UpsertEntity(ctx, a, p.ID, u)
		if err != nil {
			fail(i, u.Name, err)
			continue
		}
		count(r)
	}
	if in.Set != nil && len(in.Set.Attrs) > 0 {
		for i, slug := range in.Set.Slugs {
			e, err := s.DB.GetEntity(ctx, p.ID, slug)
			if err != nil {
				fail(i, slug, err)
				continue
			}
			attrs := map[string]any{}
			_ = json.Unmarshal(e.Attrs, &attrs)
			for k, v := range in.Set.Attrs {
				if v == nil || v == "" {
					delete(attrs, k)
				} else {
					attrs[k] = v
				}
			}
			r, err := s.UpsertEntity(ctx, a, p.ID, EntityInput{Kind: e.Kind, Name: e.Name, Slug: e.Slug, Attrs: attrs})
			if err != nil {
				fail(i, slug, err)
				continue
			}
			count(r)
		}
	}
	for i, slug := range in.Delete {
		r, err := s.DeleteEntity(ctx, a, p.ID, slug)
		if err != nil {
			fail(i, slug, err)
			continue
		}
		if r.Suggestion != nil {
			res.Suggestions++
		} else {
			res.Deleted++
		}
	}
	s.event(ctx, a, "entity.bulk", p.ID, map[string]any{"created": res.Created, "updated": res.Updated, "deleted": res.Deleted, "errors": len(res.Errors)})
	return res, nil
}
