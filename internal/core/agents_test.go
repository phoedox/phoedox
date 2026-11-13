package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

// TestAgents covers ADR-0033 stage 1: enrollment codes, reports into the
// graph, merging with manual attributes and revocation.
func TestAgents(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Homelab"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)

	if _, err := s.CreateEnrollment(ctx, reader, p.Slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers cannot enroll hosts: %v", err)
	}
	if _, err := s.CreateEnrollment(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI cannot enroll hosts: %v", err)
	}
	e, err := s.CreateEnrollment(ctx, owner, p.Slug)
	if err != nil || len(e.Code) < 20 {
		t.Fatalf("enrollment: %v %+v", err, e)
	}
	if _, err := s.EnrollAgent(ctx, "phxe_wrong", "pve1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong code: %v", err)
	}
	if _, err := s.EnrollAgent(ctx, e.Code, " "); invalidKey(err) != "validation.agent_hostname" {
		t.Fatalf("hostname required: %v", err)
	}
	en, err := s.EnrollAgent(ctx, e.Code, "pve1")
	if err != nil || en.Project != p.Slug || en.Token == "" {
		t.Fatalf("enroll: %v %+v", err, en)
	}

	// A manual entry keeps its kind, name and own attributes.
	if _, err := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "server", Name: "PVE Eins", Slug: "pve1", Attrs: map[string]any{"location": "Keller"}}); err != nil {
		t.Fatal(err)
	}
	r := Report{Hostname: "pve1", OS: "linux", Arch: "amd64", CPUs: 8, Distro: "Debian 13", MemTotal: 32 << 30, IPs: []string{"10.0.0.5"},
		Docker: []Container{{Name: "nginx", Image: "nginx:1", State: "running"}, {Name: ""}}}
	if err := s.AgentReport(ctx, "phxa_wrong", r); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong token: %v", err)
	}
	if err := s.AgentReport(ctx, en.Token, r); err != nil {
		t.Fatal(err)
	}
	host, err := s.Entity(ctx, owner, p.Slug, "pve1")
	if err != nil {
		t.Fatal(err)
	}
	attrs := map[string]any{}
	_ = json.Unmarshal(host.Entity.Attrs, &attrs)
	if host.Entity.Name != "PVE Eins" || attrs["location"] != "Keller" || attrs["ip"] != "10.0.0.5" || attrs["ram"] != 32.0 {
		t.Fatalf("merge: %+v %v", host.Entity, attrs)
	}
	c, err := s.Entity(ctx, owner, p.Slug, "pve1-nginx")
	if err != nil || c.Entity.Kind != "container" {
		t.Fatalf("container: %v %+v", err, c)
	}
	found := false
	for _, ed := range c.Edges {
		found = found || (ed.Rel == "runs_on" && ed.ToSlug == "pve1")
	}
	if !found {
		t.Fatalf("container runs_on host: %+v", c.Edges)
	}

	list, err := s.Agents(ctx, owner, p.Slug)
	if err != nil || len(list) != 1 || list[0].Hostname != "pve1" || list[0].LastSeen.IsZero() {
		t.Fatalf("list: %v %+v", err, list)
	}
	if _, err := s.Agents(ctx, reader, p.Slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers do not see agents: %v", err)
	}
	if err := s.RevokeAgent(ctx, owner, p.Slug, en.AgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.AgentReport(ctx, en.Token, r); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked token must fail: %v", err)
	}
}
