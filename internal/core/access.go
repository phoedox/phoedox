package core

import (
	"context"
	"slices"

	"github.com/phoedox/phoedox/internal/store"
)

// Scope narrows what a token or an OAuth grant may do. A nil scope means
// the full rights of the user.
type Scope struct {
	ReadOnly bool
	// Projects lists the allowed project ids; empty means all.
	Projects []string
}

// NewScope builds a scope from stored access and projects (nil if full).
func NewScope(access string, projects []string) *Scope {
	if access != store.AccessRead && len(projects) == 0 {
		return nil
	}
	return &Scope{ReadOnly: access == store.AccessRead, Projects: projects}
}

func (sc *Scope) allows(projectID string) bool {
	return sc == nil || len(sc.Projects) == 0 || slices.Contains(sc.Projects, projectID)
}

// Restricted reports whether the actor works with less than the user's
// rights (then account and admin changes are refused).
func (a Actor) Restricted() bool { return a.Scope != nil }

// AI access modes of a project (column ai_write_mode).
const (
	AIOff     = "off"     // AI clients do not see the project
	AIRead    = "read"    // AI clients may only read
	AISuggest = "suggest" // writes become suggestions (default)
	AIDirect  = "direct"  // writes apply directly
)

const settingAI = "ai_enabled"

// AIEnabled reports whether AI clients (MCP) may be used on this instance.
// Later this becomes a setting of each organization.
func (s *Service) AIEnabled(ctx context.Context) bool {
	v, err := s.DB.Setting(ctx, settingAI, "true")
	return err != nil || v != "false"
}

// InstanceSettings are the settings a system admin can change.
type InstanceSettings struct {
	AIEnabled bool `json:"aiEnabled"`
}

// Settings returns the instance settings (admins only).
func (s *Service) Settings(ctx context.Context, a Actor) (InstanceSettings, error) {
	if !a.User.IsAdmin || a.Via == "mcp" || a.Restricted() {
		return InstanceSettings{}, ErrForbidden
	}
	return InstanceSettings{AIEnabled: s.AIEnabled(ctx)}, nil
}

// UpdateSettings changes the instance settings (admins only, not via AI).
func (s *Service) UpdateSettings(ctx context.Context, a Actor, in InstanceSettings) (InstanceSettings, error) {
	if !a.User.IsAdmin || a.Via == "mcp" || a.Restricted() {
		return InstanceSettings{}, ErrForbidden
	}
	v := "false"
	if in.AIEnabled {
		v = "true"
	}
	if err := s.DB.SetSetting(ctx, settingAI, v, a.String()); err != nil {
		return InstanceSettings{}, err
	}
	s.event(ctx, a, "instance.settings", "", in)
	return in, nil
}

// TokenScope checks a requested access level and project list for a new
// token or OAuth grant and returns the project ids. Only projects the
// actor can read may be chosen.
func (s *Service) TokenScope(ctx context.Context, a Actor, access string, projects []string) (string, []string, error) {
	switch access {
	case "", store.AccessWrite:
		access = store.AccessWrite
	case store.AccessRead:
	default:
		return "", nil, invalid("validation.access", nil, "access must be read or write")
	}
	var ids []string
	for _, ref := range projects {
		p, err := s.project(ctx, Actor{User: a.User, Via: "web"}, ref, store.RoleReader)
		if err != nil {
			return "", nil, invalid("validation.project_unknown", map[string]any{"project": ref}, "unknown project %s", ref)
		}
		if !slices.Contains(ids, p.ID) {
			ids = append(ids, p.ID)
		}
	}
	return access, ids, nil
}

// errAIDisabled is returned to AI clients when AI access is switched off.
func errAIDisabled() error {
	return invalid("validation.ai_disabled", nil, "AI access is disabled on this instance")
}

// filterProjects keeps the projects the actor may see.
func (s *Service) visible(a Actor, p store.Project) bool {
	if !a.Scope.allows(p.ID) {
		return false
	}
	return a.Via != "mcp" || p.AIWriteMode != AIOff
}
