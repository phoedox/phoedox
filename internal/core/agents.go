package core

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// Agents, stage 1 (ADR-0033): hosts report read-only snapshots over HTTPS.
// An agent acts with the rights of the owner who created its enrollment
// code; events name it "agent:<id>".

const enrollmentTTL = 24 * time.Hour

func randomToken(prefix string) string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return prefix + strings.ToLower(strings.TrimRight(base32.StdEncoding.EncodeToString(b), "="))
}

// Enrollment is a code that lets hosts join a project.
type Enrollment struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
	Uses      int       `json:"uses"`
}

// CreateEnrollment makes a code for up to 20 hosts, valid for 24 hours.
func (s *Service) CreateEnrollment(ctx context.Context, a Actor, ref string) (Enrollment, error) {
	if a.Via == "mcp" || a.Plugin != "" {
		return Enrollment{}, ErrForbidden
	}
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return Enrollment{}, err
	}
	e := Enrollment{Code: randomToken("phxe_"), ExpiresAt: time.Now().Add(enrollmentTTL).UTC(), Uses: 20}
	if err := s.DB.AddEnrollment(ctx, hashToken(e.Code), p.ID, a.User.ID, store.Time{Time: e.ExpiresAt}, e.Uses); err != nil {
		return Enrollment{}, err
	}
	s.event(ctx, a, "agent.enrollment", p.ID, map[string]any{"expiresAt": e.ExpiresAt})
	return e, nil
}

// Enrolled is what an agent gets for its code.
type Enrolled struct {
	AgentID string `json:"agentId"`
	Token   string `json:"token"`
	Project string `json:"project"`
}

// EnrollAgent exchanges a code for an agent token.
func (s *Service) EnrollAgent(ctx context.Context, code, hostname string) (Enrolled, error) {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" || len(hostname) > 200 {
		return Enrolled{}, invalid("validation.agent_hostname", nil, "hostname is required")
	}
	tenant, projectID, userID, err := s.DB.UseEnrollment(ctx, hashToken(strings.TrimSpace(code)))
	if err != nil {
		return Enrolled{}, ErrForbidden
	}
	ctx = store.WithTenant(ctx, tenant)
	ag := store.Agent{ID: newID(), ProjectID: projectID, UserID: userID, Hostname: hostname, CreatedAt: store.Now()}
	token := randomToken("phxa_")
	ag.TokenHash = hashToken(token)
	if err := s.DB.CreateAgent(ctx, ag); err != nil {
		return Enrolled{}, err
	}
	p, _ := s.DB.ProjectFor(ctx, projectID, userID)
	u, _ := s.DB.UserByID(ctx, userID)
	s.event(ctx, Actor{User: u, Via: "agent", TokenID: ag.ID}, "agent.enroll", ag.ID, map[string]string{"hostname": hostname, "project": p.Slug})
	return Enrolled{AgentID: ag.ID, Token: token, Project: p.Slug}, nil
}

// Report is the snapshot an agent sends (phoedox-agent collect).
type Report struct {
	Hostname  string            `json:"hostname"`
	OS        string            `json:"os"`
	Arch      string            `json:"arch"`
	CPUs      int               `json:"cpus"`
	Kernel    string            `json:"kernel,omitempty"`
	Distro    string            `json:"distro,omitempty"`
	Virt      string            `json:"virt,omitempty"` // "", vm, lxc, container
	UptimeSec int64             `json:"uptimeSec,omitempty"`
	MemTotal  uint64            `json:"memTotalBytes,omitempty"`
	MemAvail  uint64            `json:"memAvailableBytes,omitempty"`
	IPs       []string          `json:"ips,omitempty"`
	Disks     []json.RawMessage `json:"disks,omitempty"`
	Docker    []Container       `json:"containers,omitempty"`
	Version   string            `json:"agentVersion,omitempty"`
	TakenAt   time.Time         `json:"takenAt,omitzero"`
}

// Container is a Docker container of a report.
type Container struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	State string `json:"state"`
	Ports string `json:"ports,omitempty"`
}

func gib(b uint64) float64 { return float64(b*10/(1<<30)) / 10 }

// AgentReport stores a report and updates the host's entries.
func (s *Service) AgentReport(ctx context.Context, token string, r Report) error {
	ag, err := s.DB.AgentByToken(ctx, hashToken(strings.TrimSpace(token)))
	if err != nil {
		return ErrForbidden
	}
	ctx = store.WithTenant(ctx, ag.TenantID)
	u, err := s.DB.UserByID(ctx, ag.UserID)
	if err != nil || u.Disabled {
		return ErrForbidden
	}
	a := Actor{User: u, Via: "agent", TokenID: ag.ID}
	if r.Hostname == "" {
		r.Hostname = ag.Hostname
	}
	if len(r.Docker) > 500 {
		r.Docker = r.Docker[:500]
	}
	kind := map[string]string{"vm": "vm", "lxc": "lxc"}[r.Virt]
	if kind == "" {
		kind = "server"
	}
	slug := graph.Slugify(r.Hostname)
	attrs := map[string]any{"hostname": r.Hostname, "os": strings.TrimSpace(r.Distro + " " + r.Kernel), "arch": r.Arch, "cpus": r.CPUs,
		"ram": gib(r.MemTotal), "agent_seen": time.Now().UTC().Format(time.RFC3339)}
	if len(r.IPs) > 0 {
		attrs["ip"] = r.IPs[0]
		attrs["ips"] = strings.Join(r.IPs, ", ")
	}
	host, err := s.agentUpsert(ctx, a, ag.ProjectID, kind, slug, r.Hostname, attrs)
	if err != nil {
		return err
	}
	for _, c := range r.Docker {
		if c.Name == "" {
			continue
		}
		cs := graph.Slugify(r.Hostname + "-" + c.Name)
		if _, err := s.agentUpsert(ctx, a, ag.ProjectID, "container", cs, c.Name, map[string]any{"image": c.Image, "state": c.State, "ports": c.Ports}); err == nil {
			_, _ = s.Link(ctx, a, ag.ProjectID, EdgeInput{From: cs, Rel: "runs_on", To: host})
		}
	}
	raw, _ := json.Marshal(r)
	return s.DB.SaveAgentReport(ctx, ag.ID, r.Hostname, host, r.Version, raw)
}

// agentUpsert merges reported attributes into an entry; kind and name of an
// existing entry stay as people set them.
func (s *Service) agentUpsert(ctx context.Context, a Actor, projectID, kind, slug, name string, attrs map[string]any) (string, error) {
	if e, err := s.DB.GetEntity(ctx, projectID, slug); err == nil {
		old := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &old)
		for k, v := range attrs {
			old[k] = v
		}
		kind, name, attrs = e.Kind, e.Name, old
	}
	res, err := s.UpsertEntity(ctx, a, projectID, EntityInput{Kind: kind, Name: name, Slug: slug, Attrs: attrs})
	if err != nil {
		return "", err
	}
	if res.Entity == nil {
		return "", errors.New("entity not applied")
	}
	return res.Entity.Slug, nil
}

// Agents lists the agents of a project (owners).
func (s *Service) Agents(ctx context.Context, a Actor, ref string) ([]store.Agent, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return nil, err
	}
	return s.DB.ProjectAgents(ctx, p.ID)
}

// RevokeAgent blocks an agent at once.
func (s *Service) RevokeAgent(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if err := s.DB.RevokeAgent(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "agent.revoke", id, nil)
	return nil
}
