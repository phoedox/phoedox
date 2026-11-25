package core

import (
	"context"
	"encoding/json"

	"github.com/phoedox/phoedox/internal/render"
	"github.com/phoedox/phoedox/internal/store"
)

// The server-side browser (ADR-0095): a headless Chrome service the system
// admin switches on, used for pictures of HTML screens, apps and boards and
// for PDFs of pages.

const settingRenderer = "renderer"

// Uses of the renderer, each can be switched off.
const (
	RenderScreens = "screens"
	RenderApps    = "apps"
	RenderBoards  = "boards"
	RenderPDF     = "pdf"
)

// RendererSettings are the administration's choices.
type RendererSettings struct {
	Enabled bool `json:"enabled"`
	// Off lists uses that are switched off (all are on by default).
	Off []string `json:"off,omitempty"`
}

// RendererView is what the administration shows.
type RendererView struct {
	RendererSettings
	Status render.Status `json:"status"`
	// Target is the address the browser uses to reach Phoedox.
	Target string   `json:"target,omitempty"`
	Uses   []string `json:"uses"`
}

var renderUses = []string{RenderScreens, RenderApps, RenderBoards, RenderPDF}

func (s *Service) rendererSettings(ctx context.Context) RendererSettings {
	var c RendererSettings
	if raw, _ := s.DB.Setting(store.WithTenant(ctx, store.DefaultTenant), settingRenderer, ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	return c
}

// renderer returns the renderer for a use (nil: off, not configured or the
// use is switched off).
func (s *Service) renderer(ctx context.Context, use string) *render.Renderer {
	if !s.Renderer.Configured() {
		return nil
	}
	c := s.rendererSettings(ctx)
	if !c.Enabled {
		return nil
	}
	for _, o := range c.Off {
		if o == use {
			return nil
		}
	}
	return s.Renderer
}

// RendererOn tells whether the server-side browser is on for a use.
func (s *Service) RendererOn(ctx context.Context, use string) bool {
	return s.renderer(ctx, use) != nil
}

// rendererTarget is the base address the browser opens Phoedox pages at.
func (s *Service) rendererTarget() string {
	if s.RendererTarget != "" {
		return s.RendererTarget
	}
	return s.PublicURL
}

// AdminRenderer shows the settings and whether the service answers.
func (s *Service) AdminRenderer(ctx context.Context, a Actor) (RendererView, error) {
	if err := s.adminOnly(a); err != nil {
		return RendererView{}, err
	}
	return RendererView{RendererSettings: s.rendererSettings(ctx), Status: s.Renderer.Status(ctx), Target: s.rendererTarget(), Uses: renderUses}, nil
}

// SetAdminRenderer saves them; switching on needs a reachable service.
func (s *Service) SetAdminRenderer(ctx context.Context, a Actor, in RendererSettings) (RendererView, error) {
	if err := s.adminOnly(a); err != nil {
		return RendererView{}, err
	}
	var off []string
	for _, o := range in.Off {
		for _, u := range renderUses {
			if o == u {
				off = append(off, o)
			}
		}
	}
	in.Off = off
	if in.Enabled {
		if !s.Renderer.Configured() {
			return RendererView{}, invalid("validation.renderer_missing", nil, "no renderer service is configured (PHOEDOX_RENDERER_URL)")
		}
		if st := s.Renderer.Status(ctx); !st.Reachable {
			return RendererView{}, invalid("validation.renderer_unreachable", map[string]any{"msg": st.Error}, "the renderer service does not answer: %s", st.Error)
		}
	}
	b, _ := json.Marshal(in)
	if err := s.DB.SetSetting(store.WithTenant(ctx, store.DefaultTenant), settingRenderer, string(b), a.String()); err != nil {
		return RendererView{}, err
	}
	s.event(ctx, a, "admin.renderer", "", map[string]any{"enabled": in.Enabled, "off": in.Off})
	return s.AdminRenderer(ctx, a)
}
