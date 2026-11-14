package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Documentation rules (ADR-0043): per kind, a project can require a
// location, a page that mentions the entry, a responsible person, filled
// fields and a review interval. Entries get a responsible person and their
// own review interval. Violations show on the entry and as documentation gaps.

// KindRules are the documentation rules of a kind.
type KindRules struct {
	Location   bool     `json:"location,omitempty"`
	Page       bool     `json:"page,omitempty"`
	Owner      bool     `json:"owner,omitempty"`
	Fields     []string `json:"fields,omitempty"`
	ReviewDays int      `json:"reviewDays,omitempty"`
}

func (r KindRules) empty() bool {
	return !r.Location && !r.Page && !r.Owner && len(r.Fields) == 0 && r.ReviewDays == 0
}

// placedRels give an entry a location.
var placedRels = []string{"located_in", "mounted_in", "part_of", "runs_on"}

// docState is what the rules are checked against, loaded once per project.
type docState struct {
	kinds     map[string]Kind
	mentioned map[string]bool // entity id → a live page mentions it
	connected map[string]bool // entity id → has an edge
	placed    map[string]bool // entity id → has a location edge
	onPlan    map[string]bool // entity slug → drawn on a floor plan
	personFor map[string]bool // entity id → a person entry is responsible_for it
	metas     map[string]store.EntityMeta
}

func (s *Service) docState(ctx context.Context, p store.Project) (*docState, error) {
	st := &docState{kinds: map[string]Kind{}, connected: map[string]bool{}, placed: map[string]bool{}, onPlan: map[string]bool{}, personFor: map[string]bool{}}
	edges, err := s.DB.EdgesOfProject(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if st.mentioned, err = s.DB.MentionedEntities(ctx, p.ID); err != nil {
		return nil, err
	}
	m, _ := s.model(ctx, p.ID)
	for _, k := range m.Kinds {
		st.kinds[k.ID] = k
	}
	for _, e := range edges {
		st.connected[e.From], st.connected[e.To] = true, true
		if slices.Contains(placedRels, e.Rel) {
			st.placed[e.From] = true
		}
		if e.Rel == "responsible_for" {
			st.personFor[e.To] = true
		}
	}
	if plans, err := s.DB.ListDocuments(ctx, p.ID, store.DocFilter{Type: store.DocFloorplan}); err == nil {
		for _, d := range plans {
			full, err := s.DB.GetDocument(ctx, p.ID, d.ID)
			if err != nil {
				continue
			}
			var fp Floorplan
			if json.Unmarshal(full.Content, &fp) == nil {
				for _, dev := range fp.Devices {
					st.onPlan[dev.Entity] = true
				}
			}
		}
	}
	if st.metas, err = s.DB.EntityMetas(ctx, p.ID); err != nil {
		return nil, err
	}
	return st, nil
}

func (st *docState) isPlaced(e store.Entity) bool { return st.placed[e.ID] || st.onPlan[e.Slug] }

func (st *docState) rules(kind string) KindRules {
	if r := st.kinds[kind].Rules; r != nil {
		return *r
	}
	return KindRules{}
}

// reviewDays is the entry's own interval, else its kind's.
func (st *docState) reviewDays(e store.Entity) int {
	if d := st.metas[e.ID].ReviewDays; d > 0 {
		return d
	}
	return st.rules(e.Kind).ReviewDays
}

// reviewDue: the interval after the last review or change, whichever is later.
func (st *docState) reviewDue(e store.Entity) (time.Time, bool) {
	days := st.reviewDays(e)
	if days <= 0 {
		return time.Time{}, false
	}
	base := e.UpdatedAt.Time
	if r := st.metas[e.ID].ReviewedAt; r.After(base) {
		base = r.Time
	}
	return base.AddDate(0, 0, days), true
}

// Violations lists the broken rules of an entry and the fields it misses.
type Violations struct {
	Rules   []string `json:"rules"`             // location | page | owner | fields
	Missing []string `json:"missing,omitempty"` // field names
}

func (st *docState) violations(e store.Entity, now time.Time) Violations {
	r := st.rules(e.Kind)
	v := Violations{Rules: []string{}}
	if r.empty() {
		return v
	}
	if r.Location && !st.isPlaced(e) {
		v.Rules = append(v.Rules, "location")
	}
	if r.Page && !st.mentioned[e.ID] {
		v.Rules = append(v.Rules, "page")
	}
	if r.Owner && st.metas[e.ID].OwnerID == "" && !st.personFor[e.ID] {
		v.Rules = append(v.Rules, "owner")
	}
	if len(r.Fields) > 0 {
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		for _, f := range r.Fields {
			if x, ok := attrs[f]; !ok || x == nil || x == "" {
				v.Missing = append(v.Missing, f)
			}
		}
		if len(v.Missing) > 0 {
			v.Rules = append(v.Rules, "fields")
		}
	}
	return v
}

// SetKindRules sets the documentation rules of a kind (owners).
func (s *Service) SetKindRules(ctx context.Context, a Actor, ref, kind string, in KindRules) (Kind, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return Kind{}, err
	}
	m, err := s.model(ctx, p.ID)
	if err != nil {
		return Kind{}, err
	}
	i := slices.IndexFunc(m.Kinds, func(k Kind) bool { return k.ID == kind })
	if i < 0 {
		return Kind{}, invalid("validation.kind_unknown", map[string]any{"kind": kind}, "unknown kind %q", kind)
	}
	k := m.Kinds[i]
	fields := []string{}
	for _, f := range in.Fields {
		f = strings.TrimSpace(f)
		if f == "" || slices.Contains(fields, f) {
			continue
		}
		if !slices.ContainsFunc(k.Attrs, func(at Attr) bool { return at.Name == f }) {
			return Kind{}, invalid("validation.rule_field", map[string]any{"field": f}, "the kind has no field %q", f)
		}
		fields = append(fields, f)
	}
	in.Fields = fields
	if in.ReviewDays < 0 || in.ReviewDays > 3660 {
		return Kind{}, invalid("validation.review_days", map[string]any{"max": 3660}, "review interval must be between 0 and 3660 days")
	}
	raw, _ := json.Marshal(in)
	if in.empty() {
		raw = nil
	}
	if err := s.DB.SetKindRules(ctx, p.ID, kind, raw, a.String()); err != nil {
		return Kind{}, err
	}
	s.event(ctx, a, "kind.rules", p.ID, map[string]any{"kind": kind, "rules": in})
	if in.empty() {
		k.Rules = nil
	} else {
		k.Rules = &in
	}
	return k, nil
}

// EntityMetaInfo is what the entry view shows about responsibility, review and rules.
type EntityMetaInfo struct {
	Owner       *Participant `json:"owner,omitempty"`
	OwnerID     string       `json:"ownerId,omitempty"`
	ReviewDays  int          `json:"reviewDays"`     // the entry's own interval
	KindReview  int          `json:"kindReviewDays"` // from the kind's rules
	ReviewedAt  store.Time   `json:"reviewedAt"`
	ReviewDue   string       `json:"reviewDue,omitempty"`
	Overdue     bool         `json:"overdue"`
	Rules       *KindRules   `json:"rules,omitempty"`
	Violations  Violations   `json:"violations"`
	PersonOwner bool         `json:"personOwner"` // a person entry is responsible_for it
}

// EntityMetaInput changes responsibility and review interval; nil fields stay.
type EntityMetaInput struct {
	Owner      *string `json:"owner,omitempty"` // member id or e-mail; "" removes
	ReviewDays *int    `json:"reviewDays,omitempty"`
}

func (s *Service) entityMeta(ctx context.Context, p store.Project, e store.Entity) (store.EntityMeta, error) {
	m, err := s.DB.GetEntityMeta(ctx, e.ID)
	if errors.Is(err, store.ErrNotFound) {
		return store.EntityMeta{EntityID: e.ID, ProjectID: p.ID}, nil
	}
	return m, err
}

// EntityMeta returns responsibility, review date and rule violations (readers).
func (s *Service) EntityMeta(ctx context.Context, a Actor, ref, entRef string) (EntityMetaInfo, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return EntityMetaInfo{}, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, entRef)
	if err != nil {
		return EntityMetaInfo{}, err
	}
	st, err := s.docState(ctx, p)
	if err != nil {
		return EntityMetaInfo{}, err
	}
	meta := st.metas[e.ID]
	info := EntityMetaInfo{OwnerID: meta.OwnerID, ReviewDays: meta.ReviewDays, KindReview: st.rules(e.Kind).ReviewDays, ReviewedAt: meta.ReviewedAt,
		Rules: st.kinds[e.Kind].Rules, Violations: st.violations(e, time.Now()), PersonOwner: st.personFor[e.ID]}
	if meta.OwnerID != "" {
		if u, err := s.DB.UserByID(ctx, meta.OwnerID); err == nil {
			info.Owner = &Participant{Actor: u.ID, Name: u.Name}
		}
	}
	if due, ok := st.reviewDue(e); ok {
		info.ReviewDue, info.Overdue = due.UTC().Format(time.DateOnly), !due.After(time.Now())
	}
	return info, nil
}

// SetEntityMeta sets the responsible person and review interval (editors).
// A new responsible person is notified.
func (s *Service) SetEntityMeta(ctx context.Context, a Actor, ref, entRef string, in EntityMetaInput) (EntityMetaInfo, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return EntityMetaInfo{}, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, entRef)
	if err != nil {
		return EntityMetaInfo{}, err
	}
	m, err := s.entityMeta(ctx, p, e)
	if err != nil {
		return EntityMetaInfo{}, err
	}
	before := m.OwnerID
	if in.Owner != nil {
		m.OwnerID = ""
		if ref := strings.TrimSpace(*in.Owner); ref != "" {
			id, text, err := s.assignee(ctx, p, ref)
			if err != nil {
				return EntityMetaInfo{}, err
			}
			if id == "" {
				return EntityMetaInfo{}, invalid("validation.assignee", map[string]any{"user": text}, "%q is not a member of this project", text)
			}
			m.OwnerID = id
		}
	}
	if in.ReviewDays != nil {
		if *in.ReviewDays < 0 || *in.ReviewDays > 3660 {
			return EntityMetaInfo{}, invalid("validation.review_days", map[string]any{"max": 3660}, "review interval must be between 0 and 3660 days")
		}
		m.ReviewDays, m.NotifiedAt = *in.ReviewDays, store.Time{}
	}
	m.UpdatedBy, m.UpdatedAt = a.String(), store.Now()
	if err := s.DB.SaveEntityMeta(ctx, m); err != nil {
		return EntityMetaInfo{}, err
	}
	s.event(ctx, a, "entity.meta", e.ID, map[string]any{"owner": m.OwnerID, "reviewDays": m.ReviewDays})
	if m.OwnerID != "" && m.OwnerID != before && m.OwnerID != a.User.ID {
		if u, err := s.DB.UserByID(ctx, m.OwnerID); err == nil {
			s.notify(ctx, u, Note{Kind: "entity.owner", ProjectID: p.ID, Project: p.Name, Subject: e.ID, Title: e.Name,
				Link: "/p/" + p.Slug + "/e/" + e.Slug, Actor: a.User.Name})
		}
	}
	return s.EntityMeta(ctx, a, ref, e.ID)
}

// MarkEntityReviewed records that someone checked an entry (editors).
func (s *Service) MarkEntityReviewed(ctx context.Context, a Actor, ref, entRef string) (EntityMetaInfo, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return EntityMetaInfo{}, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, entRef)
	if err != nil {
		return EntityMetaInfo{}, err
	}
	m, err := s.entityMeta(ctx, p, e)
	if err != nil {
		return EntityMetaInfo{}, err
	}
	now := store.Now()
	m.ReviewedAt, m.ReviewedBy, m.NotifiedAt, m.UpdatedBy, m.UpdatedAt = now, a.String(), store.Time{}, a.String(), now
	if err := s.DB.SaveEntityMeta(ctx, m); err != nil {
		return EntityMetaInfo{}, err
	}
	s.event(ctx, a, "entity.reviewed", e.ID, nil)
	return s.EntityMeta(ctx, a, ref, e.ID)
}

// RemindEntityReviews notifies once when an entry's review is due: its
// responsible person, else the project owners (System context, hourly).
func (s *Service) RemindEntityReviews(ctx context.Context, now time.Time) {
	sys := store.System(ctx)
	hour := now.UTC().Format("2006-01-02T15")
	if last, _ := s.DB.Setting(sys, "review.entities", ""); last == hour {
		return // once per hour is enough
	}
	_ = s.DB.SetSetting(sys, "review.entities", hour, "system")
	tenants, err := s.DB.Tenants(sys)
	if err != nil {
		return
	}
	for _, t := range tenants {
		tctx := store.WithTenant(ctx, t.ID)
		projects, err := s.DB.ListProjects(tctx, "", true)
		if err != nil {
			continue
		}
		for _, p := range projects {
			s.remindEntities(tctx, p, now)
		}
	}
}

func (s *Service) remindEntities(ctx context.Context, p store.Project, now time.Time) {
	rules, _ := s.DB.KindRules(ctx, p.ID)
	metas, _ := s.DB.EntityMetas(ctx, p.ID)
	hasReview := len(rules) > 0
	for _, m := range metas {
		if m.ReviewDays > 0 {
			hasReview = true
		}
	}
	if !hasReview {
		return
	}
	st, err := s.docState(ctx, p)
	if err != nil {
		return
	}
	ents, err := s.DB.ListEntities(ctx, p.ID, "")
	if err != nil {
		return
	}
	var owners []string
	members, _ := s.DB.ListMembers(ctx, p.ID)
	for _, m := range members {
		if m.Role == store.RoleOwner {
			owners = append(owners, m.UserID)
		}
	}
	for _, e := range ents {
		due, ok := st.reviewDue(e)
		meta := st.metas[e.ID]
		if !ok || due.After(now) || !meta.NotifiedAt.IsZero() && meta.NotifiedAt.After(due) {
			continue
		}
		to := owners
		if meta.OwnerID != "" {
			to = []string{meta.OwnerID}
		}
		for _, id := range to {
			if u, err := s.DB.UserByID(ctx, id); err == nil && !u.Disabled {
				s.notify(ctx, u, Note{Kind: "entity.review_due", ProjectID: p.ID, Project: p.Name, Subject: e.ID + ":" + due.Format(time.DateOnly), Title: e.Name,
					Link: "/p/" + p.Slug + "/e/" + e.Slug})
			}
		}
		if meta.EntityID == "" {
			meta = store.EntityMeta{EntityID: e.ID, ProjectID: p.ID, UpdatedBy: "system", UpdatedAt: store.Now()}
		}
		meta.NotifiedAt = store.Time{Time: now}
		_ = s.DB.SaveEntityMeta(ctx, meta)
		_ = s.DB.AddEvent(ctx, "system", "entity.review_due", e.ID, map[string]string{"due": due.Format(time.DateOnly)})
	}
}
