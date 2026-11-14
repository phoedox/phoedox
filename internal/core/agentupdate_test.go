package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/store"
)

func TestImageUpdatesAndAgentRelease(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)

	// a registry that serves a newer digest for nginx:1.27
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/library/nginx/manifests/1.27" {
			w.Header().Set("Docker-Content-Digest", "sha256:new")
			return
		}
		http.NotFound(w, r)
	}))
	defer reg.Close()
	pool := reg.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	s.RequestPolicy = requests.Policy{AllowPrivate: true, RootCAs: pool}
	host := strings.TrimPrefix(reg.URL, "https://")

	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Lab"})
	mk := func(slug, image, digest string) {
		attrs := map[string]any{"image": image, "image_digest": digest}
		if _, err := s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "container", Name: slug, Slug: slug, Attrs: attrs}); err != nil {
			t.Fatal(err)
		}
	}
	mk("web", host+"/library/nginx:1.27", "sha256:old")
	mk("web2", host+"/library/nginx:1.27", "sha256:new")
	mk("local", "built-here", "")
	s.CheckImageUpdates(store.System(ctx), time.Now())
	attr := func(slug string) map[string]any {
		e, _ := s.DB.GetEntity(ctx, p.ID, slug)
		m := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &m)
		return m
	}
	if a := attr("web"); a["image_update"] != true || a["image_latest"] != "sha256:new" {
		t.Fatalf("web: %v", a)
	}
	if a := attr("web2"); a["image_update"] != false {
		t.Fatalf("web2: %v", a)
	}
	if a := attr("local"); a["image_checked"] != nil {
		t.Fatalf("local image was checked: %v", a)
	}
	evs, _ := s.DB.Events(ctx, store.EventFilter{ProjectID: p.ID, Kind: "container.image_update"})
	if len(evs) != 1 {
		t.Fatalf("events: %d", len(evs))
	}

	// agent releases: proxied, verified against SHA256SUMS, cached
	bin := []byte("agent v0.3.0 linux amd64")
	sum := sha256.Sum256(bin)
	hits := 0
	rel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			http.Redirect(w, r, "/tag/v0.3.0", http.StatusFound)
		case "/download/v0.3.0/SHA256SUMS":
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  phoedox-agent-linux-amd64\n" + strings.Repeat("0", 64) + "  phoedox-agent-linux-arm64\n"))
		case "/download/v0.3.0/phoedox-agent-linux-amd64":
			hits++
			_, _ = w.Write(bin)
		case "/download/v0.3.0/phoedox-agent-linux-arm64":
			_, _ = w.Write([]byte("tampered"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer rel.Close()
	s.AgentReleases, s.AgentReleaseDir = rel.URL, t.TempDir()

	code, _ := s.CreateEnrollment(ctx, owner, p.Slug)
	en, err := s.EnrollAgent(ctx, code.Code, "nas")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AgentReport(ctx, en.Token, Report{Hostname: "nas", OS: "linux", Arch: "amd64", Version: "0.2.0"}); err != nil {
		t.Fatal(err)
	}
	acts, err := s.EntityAgentActions(ctx, owner, p.Slug, "nas")
	if err != nil {
		t.Fatal(err)
	}
	var up *AgentAction
	for i := range acts.Actions {
		if acts.Actions[i].Action == "agent.update" {
			up = &acts.Actions[i]
		}
	}
	if up == nil || up.Args[0] != "/api/v1/agent/releases/v0.3.0/phoedox-agent-linux-amd64" || up.Args[1] != hex.EncodeToString(sum[:]) {
		t.Fatalf("update action: %+v", acts.Actions)
	}
	if _, err := s.AgentRelease(ctx, "wrong", "v0.3.0", "phoedox-agent-linux-amd64"); err == nil {
		t.Fatal("release without a valid agent token")
	}
	for i := 0; i < 2; i++ {
		f, err := s.AgentRelease(ctx, en.Token, "v0.3.0", "phoedox-agent-linux-amd64")
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(f); string(b) != string(bin) {
			t.Fatalf("file: %q", b)
		}
	}
	if hits != 1 {
		t.Fatalf("downloaded %d times, want 1 (cache)", hits)
	}
	if _, err := s.AgentRelease(ctx, en.Token, "v0.3.0", "phoedox-agent-linux-arm64"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered asset: %v", err)
	}
	if _, err := s.AgentRelease(ctx, en.Token, "v0.3.0", "../../etc/passwd"); err == nil {
		t.Fatal("path traversal")
	}
	// an up-to-date agent gets no update action
	_ = s.AgentReport(ctx, en.Token, Report{Hostname: "nas", OS: "linux", Arch: "amd64", Version: "0.3.0"})
	acts, _ = s.EntityAgentActions(ctx, owner, p.Slug, "nas")
	for _, a := range acts.Actions {
		if a.Action == "agent.update" {
			t.Fatal("update offered to an up-to-date agent")
		}
	}
}
