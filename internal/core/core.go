// Package core contains the business logic: permissions, validation,
// mention extraction and the suggestion workflow for AI clients.
// HTTP and MCP are thin layers on top of it.
package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

var (
	// ErrForbidden: the actor may not do this.
	ErrForbidden = errors.New("forbidden")
	// ErrNotFound: missing or not visible (we do not reveal which).
	ErrNotFound = store.ErrNotFound
	// ErrConflict: duplicate slug or stale version.
	ErrConflict = store.ErrConflict
)

// InvalidError describes bad input. Key and Params let the web app show a
// translated message; Msg is the English fallback (also used for MCP).
type InvalidError struct {
	Key    string         `json:"key"`
	Params map[string]any `json:"params,omitempty"`
	Msg    string         `json:"error"`
}

func (e *InvalidError) Error() string { return e.Msg }

// Invalid builds an InvalidError for other packages (auth, server).
func Invalid(key string, params map[string]any, format string, args ...any) error {
	return invalid(key, params, format, args...)
}

func invalid(key string, params map[string]any, format string, args ...any) error {
	return &InvalidError{Key: key, Params: params, Msg: fmt.Sprintf(format, args...)}
}

// Actor is whoever performs an action.
type Actor struct {
	User    store.User
	Via     string // web | token | mcp | cli
	TokenID string
}

// String is the actor as stored in events and versions.
func (a Actor) String() string {
	switch a.Via {
	case "mcp":
		return "mcp:" + a.User.ID + ":" + a.TokenID
	case "token":
		return "token:" + a.User.ID + ":" + a.TokenID
	case "cli":
		return "cli"
	default:
		return "user:" + a.User.ID
	}
}

// Service bundles the core operations.
type Service struct {
	DB *store.DB
	// MaxDepth limits traversals (admin setting, default 6).
	MaxDepth int
	// Box decrypts project secrets for request blocks.
	Box *secret.Box
	// RequestPolicy limits which addresses request blocks may reach.
	RequestPolicy requests.Policy
	// OnContentChange is called when a document's content changed outside
	// the live editor (the realtime hub resets live sessions).
	OnContentChange func(ctx context.Context, documentID, by string)
}

// New returns a service with defaults.
func New(db *store.DB) *Service { return &Service{DB: db, MaxDepth: 6} }

var rank = map[string]int{store.RoleReader: 1, store.RoleEditor: 2, store.RoleOwner: 3}

// project loads a project and checks that the actor has at least role need.
func (s *Service) project(ctx context.Context, a Actor, ref, need string) (store.Project, error) {
	p, err := s.DB.ProjectFor(ctx, ref, a.User.ID)
	if err != nil {
		return p, err
	}
	if a.User.IsAdmin {
		if p.Role == "" {
			p.Role = store.RoleOwner
		}
		return p, nil
	}
	if p.Role == "" {
		return p, ErrNotFound
	}
	if rank[p.Role] < rank[need] {
		return p, ErrForbidden
	}
	return p, nil
}

func newID() string { return string(graph.NewID()) }

func (s *Service) event(ctx context.Context, a Actor, kind, subject string, payload any) {
	_ = s.DB.AddEvent(ctx, a.String(), kind, subject, payload)
}
