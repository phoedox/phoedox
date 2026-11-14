package core

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Agent actions (ADR-0036). A job is created here, signed in the browser with
// a key that never leaves the person's device, and checked by the agent
// against its own list of trusted keys and allowed actions (policy.Check).
// The server can neither sign nor widen a job.

const jobTTL = 10 * time.Minute

// Job is exactly what the agent verifies (phoedox-agent policy.Job): field
// order and names must match, the payload is json.Marshal of it.
type Job struct {
	ID      string    `json:"id"`
	Agent   string    `json:"agent"`
	Action  string    `json:"action"`
	Args    []string  `json:"args,omitempty"`
	Expires time.Time `json:"expires"`
}

// SignedJob is what the agent receives.
type SignedJob struct {
	Job       json.RawMessage `json:"job"`
	Signature string          `json:"signature"`
}

// SigningKeyInput registers a public key.
type SigningKeyInput struct {
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
}

// AddSigningKey registers a key the person created in their browser.
func (s *Service) AddSigningKey(ctx context.Context, a Actor, in SigningKeyInput) (store.SigningKey, error) {
	if a.Via == "mcp" || a.Plugin != "" || a.Restricted() {
		return store.SigningKey{}, ErrForbidden
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.PublicKey))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return store.SigningKey{}, invalid("validation.signing_key", nil, "publicKey must be a base64 ed25519 public key")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 80 {
		return store.SigningKey{}, invalid("validation.name_required", nil, "name is required")
	}
	k := store.SigningKey{ID: newID(), UserID: a.User.ID, Name: name, PublicKey: base64.StdEncoding.EncodeToString(pub), CreatedAt: store.Now()}
	if err := s.DB.AddSigningKey(ctx, k); err != nil {
		return k, err
	}
	s.event(ctx, a, "signing_key.add", k.ID, map[string]string{"name": name})
	return k, nil
}

// SigningKeys lists the actor's keys.
func (s *Service) SigningKeys(ctx context.Context, a Actor) ([]store.SigningKey, error) {
	return s.DB.SigningKeys(ctx, a.User.ID)
}

// DeleteSigningKey removes one of the actor's keys.
func (s *Service) DeleteSigningKey(ctx context.Context, a Actor, id string) error {
	if a.Via == "mcp" {
		return ErrForbidden
	}
	if err := s.DB.DeleteSigningKey(ctx, a.User.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "signing_key.delete", id, nil)
	return nil
}

// AgentAction is something an agent can do for an entry.
type AgentAction struct {
	Action string   `json:"action"`
	Args   []string `json:"args,omitempty"`
}

// EntityActions is what the entry page offers.
type EntityActions struct {
	AgentID  string           `json:"agentId,omitempty"`
	Hostname string           `json:"hostname,omitempty"`
	Actions  []AgentAction    `json:"actions"`
	Jobs     []store.AgentJob `json:"jobs"`
}

// EntityAgentActions finds the agent responsible for an entry and the
// actions it could run there. Whether the host allows them is the agent's call.
func (s *Service) EntityAgentActions(ctx context.Context, a Actor, ref, slug string) (EntityActions, error) {
	out := EntityActions{Actions: []AgentAction{}, Jobs: []store.AgentJob{}}
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return out, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, slug)
	if err != nil {
		return out, err
	}
	attrs := map[string]any{}
	_ = json.Unmarshal(e.Attrs, &attrs)
	str := func(k string) string { v, _ := attrs[k].(string); return v }
	host := e.Slug
	if e.Kind == "container" || e.Kind == "stack" {
		host = ""
		edges, _ := s.DB.EdgesOf(ctx, e.ID)
		for _, x := range edges {
			if x.From == e.ID && x.Rel == "runs_on" {
				host = x.ToSlug
			}
		}
	}
	agents, err := s.DB.ProjectAgents(ctx, p.ID)
	if err != nil {
		return out, err
	}
	i := slices.IndexFunc(agents, func(ag store.Agent) bool { return host != "" && ag.EntitySlug == host && ag.RevokedAt.IsZero() })
	if i < 0 {
		return out, nil
	}
	out.AgentID, out.Hostname = agents[i].ID, agents[i].Hostname
	switch e.Kind {
	case "container":
		if n := str("docker_name"); n != "" {
			out.Actions = append(out.Actions, AgentAction{Action: "container.restart", Args: []string{n}})
		}
		if pr, dir, svc := str("compose_project"), str("compose_dir"), str("service"); pr != "" && dir != "" && svc != "" {
			out.Actions = append(out.Actions, AgentAction{Action: "container.update", Args: []string{pr, dir, svc}})
		}
	case "stack":
		if pr, dir := str("project"), str("compose_path"); pr != "" && dir != "" {
			out.Actions = append(out.Actions, AgentAction{Action: "stack.redeploy", Args: []string{pr, dir}})
		}
	case "server", "vm", "lxc":
		out.Actions = append(out.Actions, AgentAction{Action: "system.update"})
		if up, ok := s.agentUpdateAction(ctx, agents[i]); ok {
			out.Actions = append(out.Actions, up)
		}
	}
	if out.Jobs, err = s.DB.EntityJobs(ctx, p.ID, e.Slug, 10); err != nil {
		return out, err
	}
	return out, nil
}

// CreateAgentJob prepares a job for one of the entry's actions. It runs only
// after a person signed it. AI clients may prepare jobs, never sign them.
func (s *Service) CreateAgentJob(ctx context.Context, a Actor, ref, slug, action string) (store.AgentJob, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return store.AgentJob{}, err
	}
	if a.Via == "mcp" && (p.AIWriteMode == AIOff || p.AIWriteMode == AIRead) {
		return store.AgentJob{}, ErrForbidden
	}
	acts, err := s.EntityAgentActions(ctx, a, ref, slug)
	if err != nil {
		return store.AgentJob{}, err
	}
	i := slices.IndexFunc(acts.Actions, func(x AgentAction) bool { return x.Action == action })
	if i < 0 {
		return store.AgentJob{}, invalid("validation.agent_action", map[string]any{"action": action}, "action %q is not available for this entry", action)
	}
	now := time.Now().UTC().Truncate(time.Second)
	job := Job{ID: newID(), Agent: acts.AgentID, Action: action, Args: acts.Actions[i].Args, Expires: now.Add(jobTTL)}
	payload, _ := json.Marshal(job)
	j := store.AgentJob{ID: job.ID, ProjectID: p.ID, AgentID: acts.AgentID, EntitySlug: slug, Action: action, Payload: string(payload),
		Status: "unsigned", CreatedBy: a.User.ID, CreatedAt: store.Time{Time: now}, ExpiresAt: store.Time{Time: job.Expires}}
	if err := s.DB.CreateAgentJob(ctx, j); err != nil {
		return j, err
	}
	s.event(ctx, a, "agent.job.create", j.ID, map[string]any{"action": action, "args": job.Args, "entity": slug})
	return j, nil
}

// SignAgentJob approves a job with the signature over its payload. The
// server checks the signature against the person's own keys; the agent checks
// it again against agent.yaml.
func (s *Service) SignAgentJob(ctx context.Context, a Actor, ref, id, keyID, signature string) (store.AgentJob, error) {
	if a.Via == "mcp" || a.Plugin != "" {
		return store.AgentJob{}, ErrForbidden
	}
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return store.AgentJob{}, err
	}
	j, err := s.DB.GetAgentJob(ctx, p.ID, id)
	if err != nil {
		return j, err
	}
	if j.Status != "unsigned" || !time.Now().Before(j.ExpiresAt.Time) {
		return j, invalid("validation.agent_job_state", map[string]any{"status": j.Status}, "job is %s or expired", j.Status)
	}
	keys, err := s.DB.SigningKeys(ctx, a.User.ID)
	if err != nil {
		return j, err
	}
	k := slices.IndexFunc(keys, func(x store.SigningKey) bool { return x.ID == keyID })
	sig, serr := base64.StdEncoding.DecodeString(signature)
	if k < 0 || serr != nil {
		return j, invalid("validation.agent_signature", nil, "unknown key or bad signature")
	}
	pub, _ := base64.StdEncoding.DecodeString(keys[k].PublicKey)
	if !ed25519.Verify(pub, []byte(j.Payload), sig) {
		return j, invalid("validation.agent_signature", nil, "unknown key or bad signature")
	}
	if err := s.DB.ApproveAgentJob(ctx, j.ID, keyID, signature); err != nil {
		return j, err
	}
	s.event(ctx, a, "agent.job.approve", j.ID, map[string]string{"action": j.Action, "key": keyID})
	j.Status, j.KeyID = "approved", keyID
	return j, nil
}

// AgentJobs hands approved jobs to an agent, waiting up to wait for one.
func (s *Service) AgentJobs(ctx context.Context, token string, wait time.Duration) ([]SignedJob, error) {
	ag, err := s.DB.AgentByToken(ctx, hashToken(strings.TrimSpace(token)))
	if err != nil {
		return nil, ErrForbidden
	}
	ctx = store.WithTenant(ctx, ag.TenantID)
	wait = min(max(wait, 0), 25*time.Second)
	deadline := time.Now().Add(wait)
	for {
		jobs, err := s.DB.TakeAgentJobs(ctx, ag.ID, store.Now())
		if err != nil {
			return nil, err
		}
		if len(jobs) > 0 || !time.Now().Before(deadline) {
			out := make([]SignedJob, len(jobs))
			for i, j := range jobs {
				out[i] = SignedJob{Job: json.RawMessage(j.Payload), Signature: j.Signature}
			}
			return out, nil
		}
		select {
		case <-ctx.Done():
			return []SignedJob{}, nil
		case <-time.After(time.Second):
		}
	}
}

// AgentJobResult stores what the agent reports for one of its jobs.
func (s *Service) AgentJobResult(ctx context.Context, token, id string, ok bool, output string) error {
	ag, err := s.DB.AgentByToken(ctx, hashToken(strings.TrimSpace(token)))
	if err != nil {
		return ErrForbidden
	}
	ctx = store.WithTenant(ctx, ag.TenantID)
	if len(output) > 16<<10 {
		output = output[len(output)-16<<10:]
	}
	if err := s.DB.FinishAgentJob(ctx, ag.ID, id, ok, output, store.Now()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if u, err := s.DB.UserByID(ctx, ag.UserID); err == nil {
		s.event(ctx, Actor{User: u, Via: "agent", TokenID: ag.ID}, "agent.job.result", id, map[string]any{"ok": ok})
	}
	return nil
}
