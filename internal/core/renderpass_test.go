package core

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/secret"
)

func TestRenderPass(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	if _, err := s.newRenderPass(owner.User.ID, nil, time.Minute); err == nil {
		t.Fatal("a pass without an instance key")
	}
	box, _ := secret.Load("", filepath.Join(t.TempDir(), "k"))
	s.Box = box
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Pass"})
	pass, err := s.newRenderPass(owner.User.ID, []string{p.ID}, time.Minute)
	if err != nil || !strings.HasPrefix(pass, RenderPassPrefix) {
		t.Fatalf("pass %q %v", pass, err)
	}
	a, ok := s.RenderPassActor(ctx, pass)
	if !ok || a.User.ID != owner.User.ID || a.Via != "render" || a.Scope == nil || !a.Scope.ReadOnly || len(a.Scope.Projects) != 1 {
		t.Fatalf("actor %+v %v", a, ok)
	}
	// Read-only: writing with it fails, reading works.
	if _, err := s.CreateProject(ctx, a, ProjectInput{Name: "Nope"}); err == nil {
		t.Fatal("wrote with a render pass")
	}
	if _, err := s.Project(ctx, a, p.Slug); err != nil {
		t.Fatalf("read: %v", err)
	}
	// Tampered, expired and foreign tokens are refused.
	if _, ok := s.RenderPassActor(ctx, pass[:len(pass)-3]+"abc"); ok {
		t.Fatal("tampered pass accepted")
	}
	old, _ := s.newRenderPass(owner.User.ID, nil, -time.Second)
	if _, ok := s.RenderPassActor(ctx, old); ok {
		t.Fatal("expired pass accepted")
	}
	if _, ok := s.RenderPassActor(ctx, "phx_something"); ok {
		t.Fatal("API token taken as pass")
	}
	other, _ := secret.Load("", filepath.Join(t.TempDir(), "k2"))
	s.Box = other
	if _, ok := s.RenderPassActor(ctx, pass); ok {
		t.Fatal("pass of another instance key accepted")
	}
}
