package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Render passes (ADR-0095) sign the server-side browser in when it opens a
// Phoedox page for someone: read-only, limited to projects, valid for a few
// minutes, sealed with the instance key (so every replica can check it) and
// never stored.

// RenderPassPrefix starts every render pass (sent as Bearer token).
const RenderPassPrefix = "phr_"

const renderPassContext = "render-pass"

type renderPass struct {
	User     string   `json:"u"`
	Projects []string `json:"p,omitempty"`
	Expires  int64    `json:"e"`
}

// newRenderPass seals a pass for the user, limited to the projects (ids).
func (s *Service) newRenderPass(userID string, projects []string, ttl time.Duration) (string, error) {
	if s.Box == nil {
		return "", errors.New("render pass: no instance key")
	}
	b, _ := json.Marshal(renderPass{User: userID, Projects: projects, Expires: time.Now().Add(ttl).Unix()})
	sealed, err := s.Box.Seal(b, renderPassContext)
	if err != nil {
		return "", err
	}
	return RenderPassPrefix + sealed, nil
}

// RenderPassActor checks a pass and returns its read-only actor.
func (s *Service) RenderPassActor(ctx context.Context, token string) (Actor, bool) {
	if s.Box == nil || !strings.HasPrefix(token, RenderPassPrefix) {
		return Actor{}, false
	}
	b, err := s.Box.Open(strings.TrimPrefix(token, RenderPassPrefix), renderPassContext)
	if err != nil {
		return Actor{}, false
	}
	var p renderPass
	if json.Unmarshal(b, &p) != nil || p.User == "" || time.Now().Unix() > p.Expires {
		return Actor{}, false
	}
	u, err := s.DB.UserByID(store.System(ctx), p.User)
	if err != nil || u.Disabled {
		return Actor{}, false
	}
	return Actor{User: u, Via: "render", Scope: &Scope{ReadOnly: true, Projects: p.Projects}}, true
}
