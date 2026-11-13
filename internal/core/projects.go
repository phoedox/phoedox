package core

import (
	"context"
	"regexp"
	"strings"

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
	AIWriteMode  string `json:"aiWriteMode,omitempty"` // suggest | direct
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
	if in.AIWriteMode != "suggest" && in.AIWriteMode != "direct" {
		return invalid("validation.ai_write_mode", nil, "aiWriteMode must be suggest or direct")
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
	if err := in.normalize(); err != nil {
		return store.Project{}, err
	}
	p, err := s.DB.CreateProject(ctx, store.Project{ID: newID(), Slug: in.Slug, Name: in.Name, Short: in.Short,
		Description: in.Description, PrimaryColor: in.PrimaryColor, AIWriteMode: in.AIWriteMode, OwnerID: a.User.ID})
	if err == nil {
		s.event(ctx, a, "project.create", p.ID, in)
	}
	return p, err
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
	p.Name, p.Short, p.Description, p.PrimaryColor, p.AIWriteMode = in.Name, in.Short, in.Description, in.PrimaryColor, in.AIWriteMode
	if err := s.DB.UpdateProject(ctx, p); err != nil {
		return p, err
	}
	s.event(ctx, a, "project.update", p.ID, in)
	return p, nil
}

// Projects lists projects visible to the actor.
func (s *Service) Projects(ctx context.Context, a Actor) ([]store.Project, error) {
	return s.DB.ListProjects(ctx, a.User.ID, a.User.IsAdmin)
}

// Project returns one project if the actor can read it.
func (s *Service) Project(ctx context.Context, a Actor, ref string) (store.Project, error) {
	return s.project(ctx, a, ref, store.RoleReader)
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
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if _, ok := rank[role]; !ok {
		return invalid("validation.role", nil, "role must be reader, editor or owner")
	}
	u, err := s.DB.UserByEmail(ctx, email)
	if err != nil {
		return invalid("validation.user_not_found", map[string]any{"email": email}, "no user with email %s", email)
	}
	if err := s.DB.SetMember(ctx, p.ID, u.ID, role); err != nil {
		return err
	}
	s.event(ctx, a, "project.member", p.ID, map[string]string{"user": u.ID, "role": role})
	return nil
}
