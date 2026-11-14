package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

	// Software tree: packages, Docker engine, a Portainer stack and its containers.
	r2 := Report{Hostname: "pve1", CPUs: 8, DockerVer: "28.1.1",
		Packages: []Package{{Name: "bash", Version: "5.2"}, {Name: "pve-manager", Version: "9.0.3"}, {Name: "docker-ce", Version: "5:28.0"}},
		Docker: []Container{
			{Name: "portainer", Image: "portainer/portainer-ce:2.27.1", State: "running"},
			{Name: "cloud-db", Image: "mariadb:11", State: "running", Project: "cloud", Service: "db", ComposeDir: "/data/compose/3"},
			{Name: "cloud-app", Image: "nextcloud:31", State: "exited", Project: "cloud", Service: "app", ComposeDir: "/data/compose/3"},
		}}
	if err := s.AgentReport(ctx, en.Token, r2); err != nil {
		t.Fatal(err)
	}
	rels := func(slug string) map[string]bool {
		d, err := s.Entity(ctx, owner, p.Slug, slug)
		if err != nil {
			t.Fatalf("%s: %v", slug, err)
		}
		out := map[string]bool{}
		for _, e := range d.Edges {
			if e.FromSlug == slug {
				out[e.Rel+">"+e.ToSlug] = true
			}
		}
		return out
	}
	if r := rels("pve1-sw-proxmox-ve"); !r["installed_on>pve1"] {
		t.Fatalf("proxmox: %v", r)
	}
	dk, _ := s.Entity(ctx, owner, p.Slug, "pve1-sw-docker")
	dattrs := map[string]any{}
	_ = json.Unmarshal(dk.Entity.Attrs, &dattrs)
	if dattrs["version"] != "28.1.1" || dattrs["type"] != "container_platform" {
		t.Fatalf("docker: %v", dattrs)
	}
	if r := rels("pve1-stack-cloud"); !r["runs_on>pve1"] || !r["managed_by>pve1-sw-portainer"] {
		t.Fatalf("stack: %v", r)
	}
	if r := rels("pve1-cloud-db"); !r["part_of>pve1-stack-cloud"] || !r["runs_on>pve1"] {
		t.Fatalf("container: %v", r)
	}
	if _, err := s.Entity(ctx, owner, p.Slug, "pve1-sw-bash"); err == nil {
		t.Fatal("ordinary packages are no entries")
	}
	tree, err := s.SoftwareTree(ctx, owner, p.Slug, "pve1")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var walk func(n TreeNode, depth int)
	walk = func(n TreeNode, depth int) {
		names = append(names, strings.Repeat(" ", depth)+n.Name)
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	walk(tree, 0)
	if got := strings.Join(names, "|"); !strings.Contains(got, "| cloud|  cloud-app|  cloud-db") || strings.Count(got, "cloud-db") != 1 || !strings.Contains(got, "| portainer") {
		t.Fatalf("tree: %s", got)
	}
	list0, _ := s.Agents(ctx, owner, p.Slug)
	if strings.Contains(string(list0[0].LastReport), "pve-manager") || !strings.Contains(string(list0[0].LastReport), `"packageCount":3`) {
		t.Fatalf("list without packages: %s", list0[0].LastReport)
	}
	if imageTag("ghcr.io/org/app:1.2@sha256:abc") != "1.2" || imageTag("localhost:5000/app") != "" {
		t.Fatal("imageTag")
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
