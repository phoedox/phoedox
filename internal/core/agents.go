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
	Packages  []Package         `json:"packages,omitempty"`
	DockerVer string            `json:"dockerVersion,omitempty"`
	OSID      string            `json:"osId,omitempty"`
	OSVersion string            `json:"osVersion,omitempty"`
	PkgFormat string            `json:"packageFormat,omitempty"`
	Version   string            `json:"agentVersion,omitempty"`
	TakenAt   time.Time         `json:"takenAt,omitzero"`
}

// Container is a Docker container of a report.
type Container struct {
	Name       string `json:"name"`
	Image      string `json:"image"`
	State      string `json:"state"`
	Ports      string `json:"ports,omitempty"`
	Digest     string `json:"digest,omitempty"` // registry digest of the image
	Project    string `json:"project,omitempty"`
	Service    string `json:"service,omitempty"`
	ComposeDir string `json:"composeDir,omitempty"`
}

// Package is an installed system package.
type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
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
	if len(r.Packages) > 20000 {
		r.Packages = r.Packages[:20000]
	}
	kind := map[string]string{"vm": "vm", "lxc": "lxc"}[r.Virt]
	if kind == "" {
		kind = "server"
	}
	slug := graph.Slugify(r.Hostname)
	attrs := map[string]any{"hostname": r.Hostname, "os": strings.TrimSpace(r.Distro + " " + r.Kernel), "arch": r.Arch, "cpus": r.CPUs,
		"ram": gib(r.MemTotal), "agent_seen": time.Now().UTC().Format(time.RFC3339)}
	if len(r.Packages) > 0 {
		attrs["packages"] = len(r.Packages)
	}
	if len(r.IPs) > 0 {
		attrs["ip"] = r.IPs[0]
		attrs["ips"] = strings.Join(r.IPs, ", ")
	}
	host, err := s.agentUpsert(ctx, a, ag.ProjectID, kind, slug, r.Hostname, attrs)
	if err != nil {
		return err
	}
	s.softwareTree(ctx, a, ag.ProjectID, host, r)
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
	list, err := s.DB.ProjectAgents(ctx, p.ID)
	for i := range list {
		list[i].LastReport = withoutPackages(list[i].LastReport)
	}
	return list, err
}

// withoutPackages replaces the package list of a report by its length; the
// full list stays stored for later checks (updates, CVEs).
func withoutPackages(raw store.JSON) store.JSON {
	var m map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return raw
	}
	var pk []json.RawMessage
	if json.Unmarshal(m["packages"], &pk) == nil && m["packages"] != nil {
		delete(m, "packages")
		m["packageCount"], _ = json.Marshal(len(pk))
	}
	out, _ := json.Marshal(m)
	return out
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

// notableSoftware maps package names to software entries of the software
// tree (concept: hardware → hypervisor → VM → container platform → stack).
var notableSoftware = map[string]struct{ name, typ string }{
	"docker-ce": {"Docker", "container_platform"}, "docker.io": {"Docker", "container_platform"}, "moby-engine": {"Docker", "container_platform"},
	"podman": {"Podman", "container_platform"}, "pve-manager": {"Proxmox VE", "hypervisor"}, "libvirt-daemon": {"libvirt", "hypervisor"},
	"nginx": {"nginx", "application"}, "apache2": {"Apache", "application"}, "httpd": {"Apache", "application"}, "caddy": {"Caddy", "application"},
	"haproxy": {"HAProxy", "application"}, "mariadb-server": {"MariaDB", "application"}, "mysql-server": {"MySQL", "application"},
	"redis-server": {"Redis", "application"}, "redis": {"Redis", "application"}, "samba": {"Samba", "application"},
	"wireguard-tools": {"WireGuard", "application"}, "tailscale": {"Tailscale", "application"}, "openssh-server": {"OpenSSH", "application"},
	"postfix": {"Postfix", "application"}, "zfsutils-linux": {"ZFS", "application"},
}

// softwareTree turns packages, the Docker engine, compose stacks and
// containers of a report into entries below the host.
func (s *Service) softwareTree(ctx context.Context, a Actor, projectID, host string, r Report) {
	soft := map[string]string{} // name -> slug
	add := func(name, typ, version string) {
		// Prefixes keep software, stacks and containers of the same name apart.
		slug := graph.Slugify(r.Hostname + "-sw-" + name)
		if _, ok := soft[name]; ok {
			return
		}
		if _, err := s.agentUpsert(ctx, a, projectID, "software", slug, name, map[string]any{"type": typ, "version": version}); err == nil {
			soft[name] = slug
			_, _ = s.Link(ctx, a, projectID, EdgeInput{From: slug, Rel: "installed_on", To: host})
		}
	}
	for _, p := range r.Packages {
		if n, ok := notableSoftware[p.Name]; ok {
			add(n.name, n.typ, p.Version)
		}
	}
	if r.DockerVer != "" {
		delete(soft, "Docker") // the engine's own version wins over the package version
		add("Docker", "container_platform", r.DockerVer)
	}
	portainer := false
	for _, c := range r.Docker {
		if strings.Contains(c.Image, "portainer/portainer") {
			portainer = true
			add("Portainer", "container_platform", imageTag(c.Image))
		}
	}
	stacks := map[string]string{} // project -> slug
	running := map[string]bool{}
	for _, c := range r.Docker {
		if c.Project == "" {
			continue
		}
		running[c.Project] = running[c.Project] || c.State == "running"
		if _, ok := stacks[c.Project]; ok {
			continue
		}
		slug := graph.Slugify(r.Hostname + "-stack-" + c.Project)
		attrs := map[string]any{"project": c.Project}
		if c.ComposeDir != "" {
			attrs["compose_path"] = c.ComposeDir
		}
		if _, err := s.agentUpsert(ctx, a, projectID, "stack", slug, c.Project, attrs); err != nil {
			continue
		}
		stacks[c.Project] = slug
		_, _ = s.Link(ctx, a, projectID, EdgeInput{From: slug, Rel: "runs_on", To: host})
		manager := soft["Docker"]
		if portainer && strings.HasPrefix(c.ComposeDir, "/data/compose/") {
			manager = soft["Portainer"]
		}
		if manager != "" {
			_, _ = s.Link(ctx, a, projectID, EdgeInput{From: slug, Rel: "managed_by", To: manager})
		}
	}
	for project, slug := range stacks {
		status := "stopped"
		if running[project] {
			status = "running"
		}
		_, _ = s.agentUpsert(ctx, a, projectID, "stack", slug, project, map[string]any{"status": status})
	}
	for _, c := range r.Docker {
		if c.Name == "" {
			continue
		}
		cs := graph.Slugify(r.Hostname + "-" + c.Name)
		attrs := map[string]any{"image": c.Image, "state": c.State, "ports": c.Ports, "docker_name": c.Name}
		if c.Service != "" {
			attrs["service"] = c.Service
			attrs["compose_project"] = c.Project
			attrs["compose_dir"] = c.ComposeDir
		}
		if v := imageTag(c.Image); v != "" {
			attrs["version"] = v
		}
		if c.Digest != "" {
			attrs["image_digest"] = c.Digest
			// After a pull the badge goes away without waiting for the next check.
			if e, err := s.DB.GetEntity(ctx, projectID, cs); err == nil {
				var old map[string]any
				_ = json.Unmarshal(e.Attrs, &old)
				if l, ok := old["image_latest"].(string); ok && l != "" {
					attrs["image_update"] = l != c.Digest
				}
			}
		}
		if _, err := s.agentUpsert(ctx, a, projectID, "container", cs, c.Name, attrs); err != nil {
			continue
		}
		_, _ = s.Link(ctx, a, projectID, EdgeInput{From: cs, Rel: "runs_on", To: host})
		if st := stacks[c.Project]; st != "" {
			_, _ = s.Link(ctx, a, projectID, EdgeInput{From: cs, Rel: "part_of", To: st})
		}
	}
}

// imageTag returns the tag of an image reference ("nginx:1.29" -> "1.29").
func imageTag(image string) string {
	image, _, _ = strings.Cut(image, "@")
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		return image[i+1:]
	}
	return ""
}
