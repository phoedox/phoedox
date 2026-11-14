// Package core contains the business logic: permissions, validation,
// mention extraction and the suggestion workflow for AI clients.
// HTTP and MCP are thin layers on top of it.
package core

import (
	"context"
	"errors"
	"fmt"
	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/jobs"
	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/preview"
	"github.com/phoedox/phoedox/internal/scan"
	"sync"
	"sync/atomic"
	"time"

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
	// Scope narrows the rights of tokens and OAuth grants (nil = full).
	Scope *Scope
	// Plugin is set while a plugin acts for the user; it never has more
	// rights than the user (and an AI client stays an AI client).
	Plugin string
}

// String is the actor as stored in events and versions.
func (a Actor) String() string {
	if a.Plugin != "" {
		return "plugin:" + a.User.ID + ":" + a.Plugin
	}
	switch a.Via {
	case "mcp":
		return "mcp:" + a.User.ID + ":" + a.TokenID
	case "token":
		return "token:" + a.User.ID + ":" + a.TokenID
	case "cli":
		return "cli"
	case "agent":
		return "agent:" + a.TokenID
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
	// OnDraftEnd is called when a draft was published, cancelled or its page
	// deleted (reason); live editors leave the edit mode.
	// HashPassword and VerifyPassword are set by the app (internal/auth);
	// publications use them for their passwords.
	HashPassword   func(string) (string, error)
	VerifyPassword func(hash, pw string) bool
	OnDraftEnd     func(ctx context.Context, documentID, reason, by string)
	// Mail sends reminders about forgotten drafts; PublicURL builds links in
	// them, DefaultLocale picks the language for users without one.
	Mail          mail.Sender
	PublicURL     string
	DefaultLocale string
	// Blobs stores file contents; TempDir holds uploads while they are
	// hashed; MaxUpload limits one file (bytes).
	Blobs     blob.Store
	TempDir   string
	MaxUpload int64
	// Jobs runs background work (previews, text extraction); nil runs it inline.
	Jobs *jobs.Queue
	// Preview makes preview images and extracts text (external tools optional).
	Preview preview.Tools
	// Scanner checks uploads for malware; nil skips the check.
	Scanner scan.Scanner
	// Plugins configures the plugin runtime (zero = plugins off).
	Plugins PluginConfig

	plugCache pluginCache
	pluginGen atomic.Int64
	schedMu   sync.Mutex
	schedLast map[string]time.Time
	checkMu   sync.Mutex
	vapidMu   sync.Mutex

	// SyncNotify sends ntfy and web push before notify returns (tests).
	SyncNotify bool
}

// New returns a service with defaults.
func New(db *store.DB) *Service {
	return &Service{DB: db, MaxDepth: 6, Preview: preview.Tools{MaxPixels: 60_000_000, MaxText: 2 << 20, Size: 480, MaxPages: 20, Timeout: 90 * time.Second}}
}

var rank = map[string]int{store.RoleDoc: 0, store.RoleReader: 1, store.RoleEditor: 2, store.RoleOwner: 3}

// project loads a project and checks that the actor has at least role need.
// need may be store.RoleDoc for functions that also serve people who only
// have single documents shared with them.
func (s *Service) project(ctx context.Context, a Actor, ref, need string) (store.Project, error) {
	p, err := s.DB.ProjectFor(ctx, ref, a.User.ID)
	if err != nil {
		return p, err
	}
	// Narrowed tokens and AI clients are checked before anything else,
	// also for admins.
	if !s.visible(a, p) {
		return p, ErrNotFound
	}
	if a.Via == "mcp" && (!s.AIEnabled(ctx) || (p.OrgID != "" && !p.OrgAI)) {
		return p, errAIDisabled()
	}
	if s.readOnly(a, p) && rank[need] > rank[store.RoleReader] {
		return p, ErrForbidden
	}
	if a.User.IsAdmin {
		p.Role = store.RoleOwner
		return p, nil
	}
	if p.Role == "" {
		return p, ErrNotFound
	}
	if rank[p.Role] < rank[need] {
		if p.Role == store.RoleDoc {
			return p, ErrNotFound // only single documents are shared: the rest stays invisible
		}
		return p, ErrForbidden
	}
	return p, nil
}

func (s *Service) readOnly(a Actor, p store.Project) bool {
	return (a.Scope != nil && a.Scope.ReadOnly) || (a.Via == "mcp" && p.AIWriteMode == AIRead)
}

func maxRole(a, b string) string {
	if a == "" || (b != "" && rank[b] > rank[a]) {
		return b
	}
	return a
}

// docRole is the actor's role for one document: owners see everything;
// restricted documents need a grant; grants can also raise the project role.
func (s *Service) docRole(ctx context.Context, a Actor, p store.Project, doc store.Document) (string, error) {
	if p.Role == store.RoleOwner {
		return store.RoleOwner, nil
	}
	grants, err := s.DB.DocRoles(ctx, p.ID, a.User.ID)
	if err != nil {
		return "", err
	}
	base := p.Role
	if doc.Restricted || base == store.RoleDoc {
		base = ""
	}
	return maxRole(base, grants[doc.ID]), nil
}

// docAccess loads a document and checks the actor's role for it.
func (s *Service) docAccess(ctx context.Context, a Actor, ref, docRef, need string) (store.Project, store.Document, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return p, store.Document{}, err
	}
	doc, err := s.findDoc(ctx, p.ID, docRef)
	if err != nil {
		return p, doc, err
	}
	role, err := s.docRole(ctx, a, p, doc)
	if err != nil {
		return p, doc, err
	}
	if role == "" {
		return p, store.Document{}, ErrNotFound
	}
	if rank[role] < rank[need] || (s.readOnly(a, p) && rank[need] > rank[store.RoleReader]) {
		return p, doc, ErrForbidden
	}
	p.Role = role
	return p, doc, nil
}

// docFilter tells which documents of a project the actor may see.
func (s *Service) docFilter(ctx context.Context, a Actor, p store.Project) (func(id string, restricted bool) bool, error) {
	if p.Role == store.RoleOwner {
		return func(string, bool) bool { return true }, nil
	}
	grants, err := s.DB.DocRoles(ctx, p.ID, a.User.ID)
	if err != nil {
		return nil, err
	}
	return func(id string, restricted bool) bool {
		if grants[id] != "" {
			return true
		}
		return p.Role != store.RoleDoc && !restricted
	}, nil
}

func newID() string { return string(graph.NewID()) }

// eventProject finds the project of an event: from its subject or a
// "project" field of the payload.
func (s *Service) eventProject(ctx context.Context, subject string, payload any) string {
	if p := s.DB.ProjectOfSubject(ctx, subject); p != "" {
		return p
	}
	var ref string
	switch v := payload.(type) {
	case map[string]any:
		ref, _ = v["project"].(string)
	case map[string]string:
		ref = v["project"]
	}
	if ref != "" {
		return s.DB.ProjectOfSubject(ctx, ref)
	}
	return ""
}

func (s *Service) event(ctx context.Context, a Actor, kind, subject string, payload any) {
	_ = s.DB.AddProjectEvent(ctx, a.String(), kind, subject, s.eventProject(ctx, subject, payload), payload)
	s.emitWebhooks(ctx, a, kind, subject, payload)
	s.emitPluginEvents(ctx, a, kind, subject, payload)
}
