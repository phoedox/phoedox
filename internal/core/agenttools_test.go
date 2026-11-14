package core

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestAgentTools(t *testing.T) {
	ctx := context.Background()
	s, owner, editor, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Lab"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleEditor)
	e, _ := s.CreateEnrollment(ctx, owner, p.Slug)
	en, _ := s.EnrollAgent(ctx, e.Code, "pve-01")
	if err := s.AgentReport(ctx, en.Token, Report{Hostname: "pve-01"}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "device", Name: "Desktop", Attrs: map[string]any{"mac": "AA:BB:CC:DD:EE:FF"}})

	// Wake-on-LAN: another agent sends the packet.
	acts, err := s.EntityAgentActions(ctx, editor, p.Slug, "desktop")
	if err != nil || len(acts.Actions) != 1 || acts.Actions[0].Action != "wol" || acts.Actions[0].Agent != en.AgentID || acts.Actions[0].Args[0] != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("wol: %+v %v", acts, err)
	}
	j, err := s.CreateAgentJob(ctx, editor, p.Slug, "desktop", "wol")
	if err != nil || j.AgentID != en.AgentID {
		t.Fatalf("wol job: %+v %v", j, err)
	}

	// Terminal: owners only; the session id is the job id.
	if _, err := s.CreateAgentJob(ctx, editor, p.Slug, "pve-01", "terminal"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor opened a terminal: %v", err)
	}
	tj, err := s.CreateAgentJob(ctx, owner, p.Slug, "pve-01", "terminal")
	if err != nil {
		t.Fatal(err)
	}
	var job Job
	_ = json.Unmarshal([]byte(tj.Payload), &job)
	if len(job.Args) != 1 || job.Args[0] != tj.ID {
		t.Fatalf("terminal args: %+v", job)
	}
	// Not before it is signed and only for its creator.
	if _, err := s.TerminalForBrowser(ctx, owner, p.Slug, tj.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unsigned terminal opened: %v", err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	k, err := s.AddSigningKey(ctx, owner, SigningKeyInput{Name: "laptop", PublicKey: base64.StdEncoding.EncodeToString(pub)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignAgentJob(ctx, owner, p.Slug, tj.ID, k.ID, base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(tj.Payload)))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TerminalForBrowser(ctx, owner, p.Slug, tj.ID); err != nil {
		t.Fatalf("owner: %v", err)
	}
	if _, err := s.TerminalForBrowser(ctx, editor, p.Slug, tj.ID); err == nil {
		t.Fatal("editor joined")
	}
	if _, _, err := s.TerminalForAgent(ctx, en.Token, tj.ID); err != nil {
		t.Fatalf("agent: %v", err)
	}
	if _, _, err := s.TerminalForAgent(ctx, "phx_wrong", tj.ID); err == nil {
		t.Fatal("wrong token")
	}
	s.TerminalStarted(ctx, tj)
	s.TerminalEnded(ctx, tj, nil, 3, 10, 0)
	if acts, _ := s.EntityAgentActions(ctx, owner, p.Slug, "pve-01"); len(acts.Actions) < 3 {
		t.Fatalf("host tools: %+v", acts.Actions)
	}
}
