package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/store"
)

func TestVulnerabilityCheck(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.RequestPolicy = requests.Policy{AllowPrivate: true}
	var asked []osvQuery
	osv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Queries []osvQuery `json:"queries"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		asked = append(asked, in.Queries...)
		res := make([]map[string]any, len(in.Queries))
		for i, q := range in.Queries {
			res[i] = map[string]any{}
			if q.Package.Name == "openssl" {
				res[i]["vulns"] = []map[string]string{{"id": "DSA-1"}, {"id": "CVE-2026-1"}}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": res})
	}))
	defer osv.Close()
	s.OSVURL = osv.URL

	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Lab"})
	code, _ := s.CreateEnrollment(ctx, owner, p.Slug)
	en, _ := s.EnrollAgent(ctx, code.Code, "nas")
	rep := Report{Hostname: "nas", OS: "linux", Arch: "amd64", OSID: "debian", OSVersion: "12", Packages: []Package{
		{Name: "libssl3", Version: "3.0.15-1~deb12u1", Source: "openssl"},
		{Name: "openssl", Version: "3.0.15-1~deb12u1"},
		{Name: "bash", Version: "5.2.15-2+b7"},
	}}
	if err := s.AgentReport(ctx, en.Token, rep); err != nil {
		t.Fatal(err)
	}

	// off by default: nothing leaves the instance
	s.CheckVulnerabilities(store.System(ctx), time.Now())
	if len(asked) != 0 {
		t.Fatalf("asked osv.dev while switched off: %+v", asked)
	}
	on := true
	admin := owner
	admin.User.IsAdmin = true
	if _, err := s.UpdateSettings(ctx, admin, SettingsPatch{VulnCheck: &on}); err != nil {
		t.Fatal(err)
	}
	s.CheckVulnerabilities(store.System(ctx), time.Now())
	if len(asked) != 2 || asked[0].Package.Ecosystem != "Debian:12" {
		t.Fatalf("queries (deduplicated by source): %+v", asked)
	}
	e, _ := s.DB.GetEntity(ctx, p.ID, "nas")
	var attrs struct {
		Count   int    `json:"vulnerable_packages"`
		Advis   int    `json:"vulnerabilities_count"`
		List    []Vuln `json:"vulnerabilities"`
		Checked string `json:"vulns_checked"`
	}
	_ = json.Unmarshal(e.Attrs, &attrs)
	if attrs.Count != 1 || attrs.Advis != 2 || attrs.List[0].Package != "openssl" || len(attrs.List[0].IDs) != 2 || attrs.Checked == "" {
		t.Fatalf("host attrs: %+v", attrs)
	}
	// once a day
	s.CheckVulnerabilities(store.System(ctx), time.Now().Add(time.Hour))
	if len(asked) != 2 {
		t.Fatal("checked again within a day")
	}
	for id, want := range map[[2]string]string{{"ubuntu", "24.04"}: "Ubuntu:24.04:LTS", {"ubuntu", "24.10"}: "Ubuntu:24.10", {"alpine", "3.20.3"}: "Alpine:v3.20", {"fedora", "41"}: ""} {
		if got := osvEcosystem(id[0], id[1]); got != want {
			t.Errorf("%v: %q, want %q", id, got, want)
		}
	}
}
