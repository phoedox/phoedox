package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

func TestAgentWebSocket(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := core.New(db)
	u, err := db.CreateUser(ctx, store.User{ID: "u1", Email: "o@x.de", Name: "Owner"})
	if err != nil {
		t.Fatal(err)
	}
	owner := core.Actor{User: u, Via: "web"}
	p, _ := svc.CreateProject(ctx, owner, core.ProjectInput{Name: "Lab"})
	e, _ := svc.CreateEnrollment(ctx, owner, p.Slug)
	en, err := svc.EnrollAgent(ctx, e.Code, "docker-01")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AgentReport(ctx, en.Token, core.Report{Hostname: "docker-01", Docker: []core.Container{{Name: "app", Image: "x", State: "running"}}}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(Deps{Core: svc, Auth: &auth.Auth{DB: db}}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/agent/ws"

	// A wrong token is refused before the upgrade.
	_, res, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer nope"}}})
	if err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token: %v %v", err, res)
	}

	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(dctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + en.Token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	// A signed job is pushed.
	j, err := svc.CreateAgentJob(ctx, owner, p.Slug, "docker-01-app", "container.restart")
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	k, _ := svc.AddSigningKey(ctx, owner, core.SigningKeyInput{Name: "Laptop", PublicKey: base64.StdEncoding.EncodeToString(pub)})
	if _, err := svc.SignAgentJob(ctx, owner, p.Slug, j.ID, k.ID, base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(j.Payload)))); err != nil {
		t.Fatal(err)
	}
	var m agentMsg
	for m.Type != "jobs" {
		if err := wsjson.Read(dctx, c, &m); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.Jobs) != 1 || m.Jobs[0].Signature == "" {
		t.Fatalf("jobs: %+v", m)
	}
	// The result comes back over the same connection.
	if err := wsjson.Write(dctx, c, agentMsg{Type: "result", ID: j.ID, OK: true, Output: "restarted"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if jobs, _ := svc.EntityAgentActions(ctx, owner, p.Slug, "docker-01-app"); len(jobs.Jobs) > 0 && jobs.Jobs[0].Status == "done" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("result not stored")
}
