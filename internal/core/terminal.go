package core

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Web terminal through the agent (ADR-0048). The session id is the id of a
// signed "terminal" job; the agent must allow "terminal" in agent.yaml. The
// person who created the job (a project owner) joins from the browser; every
// byte both ways is recorded and kept as a file of the entry.

// AgentFromToken finds the agent of a token and a context for its tenant.
func (s *Service) AgentFromToken(ctx context.Context, token string) (store.Agent, context.Context, error) {
	ag, err := s.DB.AgentByToken(ctx, hashToken(strings.TrimSpace(token)))
	if err != nil || !ag.RevokedAt.IsZero() {
		return store.Agent{}, ctx, ErrForbidden
	}
	return ag, store.WithTenant(ctx, ag.TenantID), nil
}

func terminalOpen(j store.AgentJob) bool {
	return j.Action == "terminal" && (j.Status == "approved" || j.Status == "sent") && j.FinishedAt.IsZero()
}

// TerminalForBrowser checks that the caller may join the terminal of a job:
// a project owner who created the job, before it ended.
func (s *Service) TerminalForBrowser(ctx context.Context, a Actor, ref, id string) (store.AgentJob, error) {
	if a.Via != "web" || a.Plugin != "" {
		return store.AgentJob{}, ErrForbidden
	}
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return store.AgentJob{}, err
	}
	j, err := s.DB.GetAgentJob(ctx, p.ID, id)
	if err != nil {
		return j, err
	}
	if !terminalOpen(j) || j.CreatedBy != a.User.ID {
		return j, ErrForbidden
	}
	return j, nil
}

// TerminalForAgent checks that the agent of the token runs this terminal job.
func (s *Service) TerminalForAgent(ctx context.Context, token, id string) (store.AgentJob, context.Context, error) {
	ag, tctx, err := s.AgentFromToken(ctx, token)
	if err != nil {
		return store.AgentJob{}, ctx, err
	}
	j, err := s.DB.GetAgentJob(tctx, ag.ProjectID, id)
	if err != nil || j.AgentID != ag.ID || !terminalOpen(j) {
		return store.AgentJob{}, tctx, ErrForbidden
	}
	return j, tctx, nil
}

// TerminalStarted writes the start event in the name of the job's creator.
func (s *Service) TerminalStarted(ctx context.Context, j store.AgentJob) {
	if u, err := s.DB.UserByID(ctx, j.CreatedBy); err == nil {
		s.event(ctx, Actor{User: u, Via: "web"}, "agent.terminal.start", j.ID, map[string]string{"entity": j.EntitySlug, "agent": j.AgentID})
	}
}

// TerminalEnded writes the end event and keeps the recording (asciicast v2)
// as a file of the entry.
func (s *Service) TerminalEnded(ctx context.Context, j store.AgentJob, recording []byte, in, out int64, took time.Duration) {
	u, err := s.DB.UserByID(ctx, j.CreatedBy)
	if err != nil {
		return
	}
	a := Actor{User: u, Via: "web"}
	meta := map[string]any{"entity": j.EntitySlug, "agent": j.AgentID, "inputBytes": in, "outputBytes": out, "seconds": int(took.Seconds())}
	if len(recording) > 0 {
		p, err := s.DB.ProjectFor(ctx, j.ProjectID, "")
		if err == nil {
			name := fmt.Sprintf("terminal-%s-%s.cast", time.Now().UTC().Format("20060102-150405"), strings.ToLower(j.ID[len(j.ID)-6:]))
			if f, err := s.UploadFile(ctx, a, p.Slug, FileInput{Name: name, Entity: j.EntitySlug}, bytes.NewReader(recording)); err == nil {
				meta["recording"] = f.ID
			}
		}
	}
	s.event(ctx, a, "agent.terminal.end", j.ID, meta)
}

// terminalActions adds the agent tools to a host entry's actions.
func terminalActions(out *EntityActions, kind string) {
	if slices.Contains([]string{"server", "vm", "lxc"}, kind) {
		out.Actions = append(out.Actions, AgentAction{Action: "speedtest"}, AgentAction{Action: "terminal"})
	}
}
