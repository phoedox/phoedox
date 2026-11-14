package core

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// TestAgentJobs covers ADR-0036: actions per entry, signing with the
// person's own key, delivery to the agent once, results.
func TestAgentJobs(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Lab"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	e, _ := s.CreateEnrollment(ctx, owner, p.Slug)
	en, _ := s.EnrollAgent(ctx, e.Code, "docker-01")
	if err := s.AgentReport(ctx, en.Token, Report{Hostname: "docker-01", Docker: []Container{
		{Name: "cloud-app-1", Image: "nextcloud:31", State: "running", Project: "cloud", Service: "app", ComposeDir: "/opt/cloud"}}}); err != nil {
		t.Fatal(err)
	}

	acts, err := s.EntityAgentActions(ctx, reader, p.Slug, "docker-01-cloud-app-1")
	if err != nil || acts.AgentID != en.AgentID || len(acts.Actions) != 2 || acts.Actions[1].Args[2] != "app" {
		t.Fatalf("container actions: %v %+v", err, acts)
	}
	if acts, _ := s.EntityAgentActions(ctx, owner, p.Slug, "docker-01-stack-cloud"); len(acts.Actions) != 1 || acts.Actions[0].Action != "stack.redeploy" {
		t.Fatalf("stack actions: %+v", acts)
	}
	if acts, _ := s.EntityAgentActions(ctx, owner, p.Slug, "docker-01"); len(acts.Actions) != 1 || acts.Actions[0].Action != "system.update" {
		t.Fatalf("host actions: %+v", acts)
	}

	if _, err := s.CreateAgentJob(ctx, reader, p.Slug, "docker-01-cloud-app-1", "container.restart"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers cannot create jobs: %v", err)
	}
	if _, err := s.CreateAgentJob(ctx, owner, p.Slug, "docker-01-cloud-app-1", "exec"); invalidKey(err) != "validation.agent_action" {
		t.Fatalf("only offered actions: %v", err)
	}
	j, err := s.CreateAgentJob(ctx, owner, p.Slug, "docker-01-cloud-app-1", "container.restart")
	if err != nil || j.Status != "unsigned" {
		t.Fatalf("create: %v %+v", err, j)
	}
	// The payload is canonical: decoding and encoding it again gives the same bytes (the agent re-encodes before verifying).
	var job Job
	_ = json.Unmarshal([]byte(j.Payload), &job)
	again, _ := json.Marshal(job)
	if string(again) != j.Payload || job.Agent != en.AgentID || job.Args[0] != "cloud-app-1" {
		t.Fatalf("payload: %s vs %s", again, j.Payload)
	}

	// Unsigned jobs are never delivered.
	if list, _ := s.AgentJobs(ctx, en.Token, 0); len(list) != 0 {
		t.Fatalf("unsigned delivered: %+v", list)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := s.AddSigningKey(ctx, owner, SigningKeyInput{Name: "Laptop", PublicKey: "abc"}); invalidKey(err) != "validation.signing_key" {
		t.Fatalf("bad key: %v", err)
	}
	if _, err := s.AddSigningKey(ctx, Actor{User: owner.User, Via: "mcp"}, SigningKeyInput{Name: "x", PublicKey: base64.StdEncoding.EncodeToString(pub)}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI cannot add keys: %v", err)
	}
	k, err := s.AddSigningKey(ctx, owner, SigningKeyInput{Name: "Laptop", PublicKey: base64.StdEncoding.EncodeToString(pub)})
	if err != nil {
		t.Fatal(err)
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := s.SignAgentJob(ctx, owner, p.Slug, j.ID, k.ID, base64.StdEncoding.EncodeToString(ed25519.Sign(other, []byte(j.Payload)))); invalidKey(err) != "validation.agent_signature" {
		t.Fatalf("wrong key: %v", err)
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(j.Payload)))
	if _, err := s.SignAgentJob(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug, j.ID, k.ID, sig); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI cannot sign: %v", err)
	}
	if _, err := s.SignAgentJob(ctx, owner, p.Slug, j.ID, k.ID, sig); err != nil {
		t.Fatal(err)
	}

	list, err := s.AgentJobs(ctx, en.Token, time.Second)
	if err != nil || len(list) != 1 || list[0].Signature != sig || string(list[0].Job) != j.Payload {
		t.Fatalf("delivery: %v %+v", err, list)
	}
	if list, _ := s.AgentJobs(ctx, en.Token, 0); len(list) != 0 {
		t.Fatal("a job is delivered once")
	}
	if err := s.AgentJobResult(ctx, "phxa_wrong", j.ID, true, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong token: %v", err)
	}
	if err := s.AgentJobResult(ctx, en.Token, j.ID, true, "restarted cloud-app-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.AgentJobResult(ctx, en.Token, j.ID, false, "again"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("results are final: %v", err)
	}
	acts, _ = s.EntityAgentActions(ctx, owner, p.Slug, "docker-01-cloud-app-1")
	if len(acts.Jobs) != 1 || acts.Jobs[0].Status != "done" || acts.Jobs[0].Output != "restarted cloud-app-1" {
		t.Fatalf("history: %+v", acts.Jobs)
	}
}
