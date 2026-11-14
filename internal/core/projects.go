package core

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

var colorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// ProjectInput creates or updates a project.
type ProjectInput struct {
	Name         string `json:"name"`
	Slug         string `json:"slug,omitempty"`
	Short        string `json:"short,omitempty"`
	Description  string `json:"description,omitempty"`
	PrimaryColor string `json:"primaryColor,omitempty"`
	AIWriteMode  string `json:"aiWriteMode,omitempty"` // off | read | suggest | direct
	// Org is the organization (slug or id); "" = personal, nil = unchanged.
	Org *string `json:"org,omitempty"`
	// MainLocale and Locales make the project multilingual (ADR-0028); nil = unchanged.
	MainLocale *string   `json:"mainLocale,omitempty"`
	Locales    *[]string `json:"locales,omitempty"`
	// Features switches optional tabs on (ADR-0032); nil = unchanged (none for new projects).
	Features *[]string `json:"features,omitempty"`
	// Template fills a new project with starting pages and boards (id from
	// GET /templates), in TemplateLocale ("de" or "en").
	Template       string `json:"template,omitempty"`
	TemplateLocale string `json:"templateLocale,omitempty"`
}

// Features are the optional tabs of a project.
var Features = []string{"decisions", "graph", "model", "files"}

func normalizeFeatures(in []string) ([]string, error) {
	out := []string{}
	for _, f := range Features {
		if slices.Contains(in, f) {
			out = append(out, f)
		}
	}
	for _, f := range in {
		if !slices.Contains(Features, f) {
			return nil, invalid("validation.feature", map[string]any{"feature": f}, "unknown feature %q", f)
		}
	}
	return out, nil
}

// orgFor resolves the organization a project goes to; the actor must be a member.
func (s *Service) orgFor(ctx context.Context, a Actor, ref *string) (string, error) {
	if ref == nil || *ref == "" {
		return "", nil
	}
	o, err := s.org(ctx, a, *ref, "")
	if err != nil {
		return "", invalid("validation.org_unknown", map[string]any{"org": *ref}, "organization %q not found", *ref)
	}
	return o.ID, nil
}

func (in *ProjectInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return invalid("validation.name_required", nil, "name is required")
	}
	if in.Slug == "" {
		in.Slug = graph.Slugify(in.Name)
	}
	if err := graph.ValidateSlug(in.Slug); err != nil {
		return invalid("validation.slug", nil, "slug: %v", err)
	}
	if in.Short == "" {
		in.Short = shortOf(in.Name)
	}
	in.Short = strings.ToUpper(in.Short)
	if len([]rune(in.Short)) > 3 {
		return invalid("validation.short", nil, "short must have at most 3 characters")
	}
	if in.PrimaryColor == "" {
		in.PrimaryColor = "#F26A1B"
	}
	if !colorRe.MatchString(in.PrimaryColor) {
		return invalid("validation.color", nil, "primaryColor must look like #F26A1B")
	}
	if in.AIWriteMode == "" {
		in.AIWriteMode = "suggest"
	}
	switch in.AIWriteMode {
	case AIOff, AIRead, AISuggest, AIDirect:
	default:
		return invalid("validation.ai_write_mode", nil, "aiWriteMode must be off, read, suggest or direct")
	}
	return nil
}

func shortOf(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		out = append(out, []rune(w)[0])
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 1 && len([]rune(name)) > 1 {
		out = append(out, []rune(name)[1])
	}
	return string(out)
}

// CreateProject creates a project owned by the actor.
func (s *Service) CreateProject(ctx context.Context, a Actor, in ProjectInput) (store.Project, error) {
	if a.Via == "mcp" && !s.AIEnabled(ctx) {
		return store.Project{}, errAIDisabled()
	}
	if a.Restricted() {
		return store.Project{}, ErrForbidden
	}
	if err := in.normalize(); err != nil {
		return store.Project{}, err
	}
	orgID, err := s.orgFor(ctx, a, in.Org)
	if err != nil {
		return store.Project{}, err
	}
	features := []string{}
	var tpl Template
	if in.Template != "" {
		var ok bool
		if tpl, ok = loadTemplate(in.Template, in.TemplateLocale); !ok {
			return store.Project{}, invalid("validation.template", map[string]any{"template": in.Template}, "unknown template %q", in.Template)
		}
		if in.Features == nil {
			in.Features = &tpl.Features
		}
	}
	if in.Features != nil {
		if features, err = normalizeFeatures(*in.Features); err != nil {
			return store.Project{}, err
		}
	}
	p, err := s.DB.CreateProject(ctx, store.Project{ID: newID(), Slug: in.Slug, Name: in.Name, Short: in.Short,
		Description: in.Description, PrimaryColor: in.PrimaryColor, AIWriteMode: in.AIWriteMode, OwnerID: a.User.ID, OrgID: orgID, Features: features})
	if err != nil {
		return p, err
	}
	s.event(ctx, a, "project.create", p.ID, in)
	if tpl.ID != "" {
		s.applyTemplate(ctx, a, p, tpl)
	}
	if full, err := s.DB.ProjectFor(ctx, p.ID, a.User.ID); err == nil {
		full.Role = store.RoleOwner
		p = full
	}
	return p, nil
}

// UpdateProject changes settings (owner only). The slug stays.
func (s *Service) UpdateProject(ctx context.Context, a Actor, ref string, in ProjectInput) (store.Project, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return p, err
	}
	in.Slug = p.Slug
	if err := in.normalize(); err != nil {
		return p, err
	}
	if a.Via == "mcp" && in.AIWriteMode != p.AIWriteMode {
		return p, ErrForbidden // AI clients cannot widen their own access
	}
	if in.Org != nil {
		if a.Via == "mcp" {
			return p, ErrForbidden
		}
		orgID, err := s.orgFor(ctx, a, in.Org)
		if err != nil {
			return p, err
		}
		p.OrgID = orgID
	}
	p.Name, p.Short, p.Description, p.PrimaryColor, p.AIWriteMode = in.Name, in.Short, in.Description, in.PrimaryColor, in.AIWriteMode
	if in.MainLocale != nil || in.Locales != nil {
		main, others := p.MainLocale, p.Locales
		if in.MainLocale != nil {
			main = *in.MainLocale
		}
		if in.Locales != nil {
			others = *in.Locales
		}
		if p.MainLocale, p.Locales, err = normalizeLocales(main, others); err != nil {
			return p, err
		}
	}
	if in.Features != nil {
		if p.Features, err = normalizeFeatures(*in.Features); err != nil {
			return p, err
		}
	}
	if err := s.DB.UpdateProject(ctx, p); err != nil {
		return p, err
	}
	s.event(ctx, a, "project.update", p.ID, in)
	return p, nil
}

// Projects lists projects visible to the actor.
func (s *Service) Projects(ctx context.Context, a Actor) ([]store.Project, error) {
	if a.Via == "mcp" && !s.AIEnabled(ctx) {
		return nil, errAIDisabled()
	}
	ps, err := s.DB.ListProjects(ctx, a.User.ID, a.User.IsAdmin)
	if err != nil {
		return nil, err
	}
	out := make([]store.Project, 0, len(ps))
	for _, p := range ps {
		if !s.visible(a, p) || (a.Via == "mcp" && p.OrgID != "" && !p.OrgAI) {
			continue
		}
		if a.User.IsAdmin {
			p.Role = store.RoleOwner
		}
		out = append(out, p)
	}
	return out, nil
}

// Project returns one project if the actor can read it.
func (s *Service) Project(ctx context.Context, a Actor, ref string) (store.Project, error) {
	return s.project(ctx, a, ref, store.RoleDoc)
}

// Members lists members (readers may see them).
func (s *Service) Members(ctx context.Context, a Actor, ref string) ([]store.Member, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return s.DB.ListMembers(ctx, p.ID)
}

// SetMember adds or changes a member by email (owner only).
func (s *Service) SetMember(ctx context.Context, a Actor, ref, email, role string) error {
	return s.SetMemberUntil(ctx, a, ref, email, role, time.Time{})
}

// SetMemberUntil adds or changes a member; with an expiry date the member is
// a guest whose access ends then (owner only, not for the owner role).
func (s *Service) SetMemberUntil(ctx context.Context, a Actor, ref, email, role string, until time.Time) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if a.Via == "mcp" {
		return ErrForbidden
	}
	if role != store.RoleReader && role != store.RoleEditor && role != store.RoleOwner {
		return invalid("validation.role", nil, "role must be reader, editor or owner")
	}
	if !until.IsZero() && (role == store.RoleOwner || !until.After(time.Now())) {
		return invalid("validation.guest_until", nil, "guests need a future date and cannot be owners")
	}
	u, err := s.DB.UserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		return invalid("validation.user_not_found", map[string]any{"email": email}, "no user with email %s", email)
	}
	if role != store.RoleOwner || !until.IsZero() {
		if err := s.keepOwner(ctx, p.ID, u.ID); err != nil {
			return err
		}
	}
	var exp store.Time
	if !until.IsZero() {
		exp = store.Time{Time: until.UTC()}
	}
	if err := s.DB.SetMember(ctx, p.ID, u.ID, role, exp); err != nil {
		return err
	}
	s.event(ctx, a, "project.member", p.ID, map[string]any{"user": u.ID, "role": role, "until": exp})
	return nil
}

// keepOwner refuses to take away the last permanent owner of a project.
func (s *Service) keepOwner(ctx context.Context, projectID, userID string) error {
	ms, err := s.DB.ListMembers(ctx, projectID)
	if err != nil {
		return err
	}
	for _, m := range ms {
		if m.UserID == userID && m.Role == store.RoleOwner && m.ExpiresAt.IsZero() {
			if n, err := s.DB.CountOwners(ctx, projectID); err != nil || n <= 1 {
				return invalid("validation.last_owner", nil, "a project needs at least one owner")
			}
		}
	}
	return nil
}

// RemoveMember removes a direct member (owner only).
func (s *Service) RemoveMember(ctx context.Context, a Actor, ref, userID string) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if a.Via == "mcp" {
		return ErrForbidden
	}
	if err := s.keepOwner(ctx, p.ID, userID); err != nil {
		return err
	}
	if err := s.DB.RemoveMember(ctx, p.ID, userID); err != nil {
		return err
	}
	s.event(ctx, a, "project.member_remove", p.ID, map[string]string{"user": userID})
	return nil
}
