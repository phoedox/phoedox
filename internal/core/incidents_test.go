package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

// TestIncidents covers automatic and manual incidents, the public status
// page and its uptime.
func TestIncidents(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	doc, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Web", Markdown: "```request\nGET https://example.org/\n```\n"})
	yes := true
	c, err := s.CreateCheck(ctx, owner, p.Slug, CheckInput{Document: doc.Document.Slug, Name: "Website", IntervalSec: 60, Public: &yes})
	if err != nil || !c.Public {
		t.Fatalf("check: %v %+v", err, c)
	}
	chk, _ := s.DB.GetCheck(ctx, p.ID, c.ID)

	// A failing check opens a public incident, recovery resolves it.
	chk.LastStatus, chk.LastDetail = store.CheckFail, "HTTP 502"
	s.checkIncident(ctx, owner, chk)
	s.checkIncident(ctx, owner, chk) // still failing: no second incident
	list, err := s.Incidents(ctx, reader, p.Slug)
	if err != nil || len(list) != 1 || !list[0].Public || list[0].Status != store.IncidentInvestigating || list[0].Updates[0].Body != "HTTP 502" || !list[0].Updates[0].Auto {
		t.Fatalf("auto incident: %v %+v", err, list)
	}
	chk.LastStatus, chk.LastDetail = store.CheckOK, "HTTP 200"
	s.checkIncident(ctx, owner, chk)
	list, _ = s.Incidents(ctx, reader, p.Slug)
	if list[0].Status != store.IncidentResolved || list[0].ResolvedAt.IsZero() || len(list[0].Updates) != 2 {
		t.Fatalf("resolved: %+v", list[0])
	}

	// Manual incidents: editors only, valid states.
	if _, err := s.CreateIncident(ctx, reader, p.Slug, IncidentInput{Title: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader: %v", err)
	}
	if _, err := s.CreateIncident(ctx, owner, p.Slug, IncidentInput{Title: "x", Status: "panic"}); invalidKey(err) != "validation.incident_status" {
		t.Fatalf("status: %v", err)
	}
	inc, err := s.CreateIncident(ctx, owner, p.Slug, IncidentInput{Title: "Wartung DSL", Body: "Provider arbeitet an der Leitung"})
	if err != nil || inc.Public {
		t.Fatalf("manual: %v %+v", err, inc)
	}
	inc, err = s.AddIncidentUpdate(ctx, owner, p.Slug, inc.ID, IncidentUpdateInput{Status: store.IncidentIdentified, Body: "Kabel beschädigt"})
	if err != nil || inc.Status != store.IncidentIdentified || len(inc.Updates) != 2 {
		t.Fatalf("update: %v %+v", err, inc)
	}

	// Status page: off until the owner turns it on; private incidents stay private.
	if sp, _ := s.ProjectStatusPage(ctx, owner, p.Slug); sp.Enabled {
		t.Fatal("status page on by default")
	}
	if _, err := s.SetProjectStatusPage(ctx, reader, p.Slug, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader turns on: %v", err)
	}
	sp, err := s.SetProjectStatusPage(ctx, owner, p.Slug, true)
	if err != nil || !sp.Enabled || !strings.HasPrefix(sp.Path, "/status/") {
		t.Fatalf("on: %v %+v", err, sp)
	}
	token := strings.TrimPrefix(sp.Path, "/status/")
	page, _, err := s.StatusByToken(ctx, token, time.Now())
	if err != nil || page.Title != "Haus" || len(page.Checks) != 1 || len(page.Incidents) != 1 || page.Overall != "operational" {
		t.Fatalf("page: %v %+v", err, page)
	}
	if page.Incidents[0].CreatedBy != "" || page.Incidents[0].Updates[0].CreatedBy != "" {
		t.Fatal("status page shows who wrote updates")
	}
	if page.Checks[0].Uptime >= 100 || page.Checks[0].Uptime < 99 {
		t.Fatalf("uptime: %v", page.Checks[0].Uptime)
	}
	// Making the manual one public shows it as ongoing.
	if _, err := s.UpdateIncident(ctx, owner, p.Slug, inc.ID, IncidentInput{Title: "Wartung DSL", Public: true}); err != nil {
		t.Fatal(err)
	}
	if page, _, _ = s.StatusByToken(ctx, token, time.Now()); page.Overall != "degraded" || len(page.Incidents) != 2 {
		t.Fatalf("degraded: %+v", page)
	}
	// Turning it off makes the address unknown.
	if _, err := s.SetProjectStatusPage(ctx, owner, p.Slug, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.StatusByToken(ctx, token, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after off: %v", err)
	}
	if err := s.DeleteIncident(ctx, owner, p.Slug, inc.ID); err != nil {
		t.Fatal(err)
	}
}

func TestUptime(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	since := now.Add(-statusWindow)
	at := func(d time.Duration) store.Time { return store.Time{Time: now.Add(-d)} }
	list := []store.Incident{
		{CheckID: "c", StartedAt: at(72 * time.Hour), ResolvedAt: at(60 * time.Hour)},                      // 12 h
		{CheckID: "c", StartedAt: at(40 * 24 * time.Hour), ResolvedAt: at(29*24*time.Hour + 12*time.Hour)}, // 12 h inside the window
		{CheckID: "other", StartedAt: at(time.Hour)},
	}
	if got := uptime(list, "c", since, now); got != 96.66 {
		t.Fatalf("uptime %v", got)
	}
	if got := uptime(nil, "c", since, now); got != 100 {
		t.Fatalf("no incidents %v", got)
	}
}

// TestMatrix: the token is write-only and sealed; messages go to the room.
func TestMatrix(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.Box, _ = secret.New(make([]byte, 32))
	var got struct {
		path, auth string
		body       map[string]string
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.auth = r.URL.EscapedPath(), r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got.body)
		_, _ = w.Write([]byte(`{"event_id":"$1"}`))
	}))
	defer srv.Close()
	pool := srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	s.RequestPolicy = requests.Policy{AllowPrivate: true, RootCAs: pool}

	bad := NotificationSettingsInput{NotificationSettings: store.NotificationSettings{Email: "off", MatrixHomeserver: "http://x", MatrixRoom: "!a:x"}}
	if _, err := s.SaveNotificationSettings(ctx, owner, bad); invalidKey(err) != "validation.notify_matrix" {
		t.Fatalf("http homeserver: %v", err)
	}
	token := "syt_secret"
	v, err := s.SaveNotificationSettings(ctx, owner, NotificationSettingsInput{
		NotificationSettings: store.NotificationSettings{Email: "off", MatrixHomeserver: srv.URL + "/", MatrixRoom: "!room:example.org"}, MatrixAccessToken: &token})
	if err != nil || !v.MatrixConnected || v.MatrixHomeserver != srv.URL {
		t.Fatalf("save: %v %+v", err, v)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "syt_secret") {
		t.Fatal("token leaks")
	}
	set, _ := s.DB.GetNotificationSettings(ctx, owner.User.ID)
	if strings.Contains(set.MatrixToken, "syt_secret") {
		t.Fatal("token stored in plain text")
	}
	// Saving again without a token keeps it.
	if v, _ = s.SaveNotificationSettings(ctx, owner, NotificationSettingsInput{NotificationSettings: set}); !v.MatrixConnected {
		t.Fatal("token lost")
	}
	if err := s.sendMatrix(ctx, set, "Website gestört", "HTTP 502", "https://phx.test/p/haus"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.path, "/_matrix/client/v3/rooms/%21room:example.org/send/m.room.message/") || got.auth != "Bearer syt_secret" ||
		got.body["msgtype"] != "m.notice" || !strings.Contains(got.body["body"], "HTTP 502") {
		t.Fatalf("request: %+v", got)
	}
}
