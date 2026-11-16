package botcheck

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/secret"
)

type memStore struct {
	mu       sync.Mutex
	settings map[string]string
	used     map[string]bool
	events   []Event
}

func (m *memStore) Setting(_ context.Context, k, def string) (string, error) {
	if v, ok := m.settings[k]; ok {
		return v, nil
	}
	return def, nil
}
func (m *memStore) SetSetting(_ context.Context, k, v, _ string) error { m.settings[k] = v; return nil }
func (m *memStore) MarkBotUsed(_ context.Context, id string, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.used[id] {
		return false, nil
	}
	m.used[id] = true
	return true, nil
}
func (m *memStore) AddBotEvent(_ context.Context, e Event) error {
	m.events = append(m.events, e)
	return nil
}
func (m *memStore) BotEvents(_ context.Context, since time.Time, outcome string, _ int) ([]Event, error) {
	out := []Event{}
	for _, e := range m.events {
		if !e.At.Before(since) && (outcome == "" || e.Outcome == outcome) {
			out = append(out, e)
		}
	}
	return out, nil
}
func (m *memStore) PurgeBot(context.Context, time.Time) error { return nil }

func newTest(t *testing.T) (*Service, *memStore, *[]string) {
	box, err := secret.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	st := &memStore{settings: map[string]string{}, used: map[string]bool{}}
	mails := &[]string{}
	s := New(st, box, func(_ context.Context, to, code, _ string) error { *mails = append(*mails, to+":"+code); return nil })
	return s, st, mails
}

func solvePow(prefix string, diff int) string {
	for i := 0; ; i++ {
		n := strconv.Itoa(i)
		if powOK(prefix, n, diff) {
			return n
		}
	}
}

var human = Signals{ElapsedMS: 4000, PointerMoves: 40, PointerDist: 900, Keys: 12, KeyVariance: 900, Languages: 2, Behaviour: 0.8}

func TestPassAndToken(t *testing.T) {
	ctx := context.Background()
	s, st, _ := newTest(t)
	ch, err := s.Issue(ctx, PlaceSignup, "ip1")
	if err != nil || ch.Off || ch.Difficulty != 16 || ch.Provider != ProviderOwn {
		t.Fatalf("issue: %v %+v", err, ch)
	}
	nonce := solvePow(ch.Prefix, ch.Difficulty)
	r, err := s.Verify(ctx, VerifyInput{Challenge: ch.Token, Nonce: nonce, Signals: human}, "ip1")
	if err != nil || r.Token == "" {
		t.Fatalf("verify: %v %+v", err, r)
	}
	// The challenge is single use.
	if _, err := s.Verify(ctx, VerifyInput{Challenge: ch.Token, Nonce: nonce, Signals: human}, "ip1"); err == nil {
		t.Fatal("challenge reused")
	}
	if err := s.Check(ctx, PlaceReset, r.Token); err == nil {
		t.Fatal("token for another place")
	}
	if err := s.Check(ctx, PlaceSignup, r.Token); err != nil {
		t.Fatalf("check: %v", err)
	}
	if err := s.Check(ctx, PlaceSignup, r.Token); err == nil {
		t.Fatal("token reused")
	}
	if err := s.Check(ctx, PlaceSignup, ""); err != ErrRequired {
		t.Fatalf("missing token: %v", err)
	}
	if len(st.events) != 1 || st.events[0].Outcome != "passed" {
		t.Fatalf("events: %+v", st.events)
	}
	// A wrong proof of work is blocked.
	ch2, _ := s.Issue(ctx, PlaceSignup, "ip1")
	if r, _ := s.Verify(ctx, VerifyInput{Challenge: ch2.Token, Nonce: "x", Signals: human}, "ip1"); !r.Blocked {
		t.Fatalf("bad pow: %+v", r)
	}
}

func TestPuzzleAndEmail(t *testing.T) {
	ctx := context.Background()
	s, _, mails := newTest(t)
	// No interaction and too fast: a visible challenge.
	ch, _ := s.Issue(ctx, PlaceSignup, "ip2")
	bot := Signals{ElapsedMS: 3000, Languages: 1}
	r, err := s.Verify(ctx, VerifyInput{Challenge: ch.Token, Nonce: solvePow(ch.Prefix, ch.Difficulty), Signals: bot}, "ip2")
	if err != nil || r.Visible == nil || !r.Email || !strings.HasPrefix(r.Visible.Image, "data:image/png;base64,") {
		t.Fatalf("visible: %v %+v", err, r)
	}
	var v challenge
	if err := s.open(r.Visible.Token, "visible", &v); err != nil {
		t.Fatal(err)
	}
	wrong := v.Answer + 90
	r2, err := s.Solve(ctx, SolveInput{Challenge: r.Visible.Token, Value: wrong}, "ip2")
	if err != nil || !r2.Retry || r2.Visible == nil {
		t.Fatalf("wrong answer: %v %+v", err, r2)
	}
	var v2 challenge
	_ = s.open(r2.Visible.Token, "visible", &v2)
	r3, err := s.Solve(ctx, SolveInput{Challenge: r2.Visible.Token, Value: v2.Answer + 3}, "ip2")
	if err != nil || r3.Token == "" {
		t.Fatalf("right answer: %v %+v", err, r3)
	}
	// Honeypot: blocked, e-mail code offered.
	ch3, _ := s.Issue(ctx, PlaceSignup, "ip3")
	r4, _ := s.Verify(ctx, VerifyInput{Challenge: ch3.Token, Nonce: solvePow(ch3.Prefix, ch3.Difficulty), Signals: Signals{Honeypot: true}}, "ip3")
	if !r4.Blocked || !r4.Email {
		t.Fatalf("honeypot: %+v", r4)
	}
	e, err := s.EmailCode(ctx, EmailInput{Challenge: ch3.Token, Email: "A@Example.org"}, "ip3")
	if err != nil || e.Visible == nil || len(*mails) != 1 {
		t.Fatalf("email: %v %+v %v", err, e, *mails)
	}
	code := strings.Split((*mails)[0], ":")[1]
	bad, _ := s.EmailVerify(ctx, EmailInput{Challenge: e.Visible.Token, Code: "000000x"}, "ip3")
	if !bad.Retry {
		t.Fatalf("wrong code: %+v", bad)
	}
	ok, err := s.EmailVerify(ctx, EmailInput{Challenge: bad.Visible.Token, Code: code}, "ip3")
	if err != nil || ok.Token == "" {
		t.Fatalf("code: %v %+v", err, ok)
	}
}

func TestConfig(t *testing.T) {
	ctx := context.Background()
	s, st, _ := newTest(t)
	c := s.Config(ctx)
	c.Places[PlaceSignup] = ""
	c.Providers[ProviderTurnstile] = &ProviderKeys{Enabled: true, SiteKey: "site", Secret: "secret-value"}
	if _, err := s.SaveConfig(ctx, c, "admin"); err == nil {
		t.Fatal("third-party providers need a DPA confirmation")
	}
	c.Providers[ProviderTurnstile].DPAConfirmed = true
	c.Policies = append(c.Policies, Policy{ID: "ts", Name: "Turnstile", Provider: ProviderTurnstile, PassAbove: 0.5, BlockBelow: 0.1, Visible: "none"})
	c.Places[PlaceReset] = "ts"
	out, err := s.SaveConfig(ctx, c, "admin")
	if err != nil || out.Providers[ProviderTurnstile].Secret != "" || !out.Providers[ProviderTurnstile].HasSecret {
		t.Fatalf("save: %v %+v", err, out.Providers[ProviderTurnstile])
	}
	if strings.Contains(st.settings[settingKey], "secret-value") {
		t.Fatal("secret stored in plain text")
	}
	// Saving again without the secret keeps it.
	c2 := s.Config(ctx)
	c2.Providers[ProviderTurnstile].Secret = ""
	if _, err := s.SaveConfig(ctx, Public(c2), "admin"); err != nil {
		t.Fatal(err)
	}
	if !s.Config(ctx).Providers[ProviderTurnstile].HasSecret {
		t.Fatal("secret lost")
	}
	if ch, _ := s.Issue(ctx, PlaceSignup, "x"); !ch.Off {
		t.Fatal("place off")
	}
	if ch, _ := s.Issue(ctx, PlaceReset, "x"); ch.Provider != ProviderTurnstile || ch.SiteKey != "site" {
		t.Fatalf("provider challenge: %+v", ch)
	}
	if err := s.Off(ctx, "cli"); err != nil {
		t.Fatal(err)
	}
	for _, p := range Places {
		if s.Enabled(ctx, p) {
			t.Fatalf("%s still on", p)
		}
	}
	var raw Config
	_ = json.Unmarshal([]byte(st.settings[settingKey]), &raw)
	if len(raw.Policies) < 4 {
		t.Fatal("presets kept")
	}
}

func TestScore(t *testing.T) {
	if s, _ := Score(human); s < 0.9 {
		t.Fatalf("human: %v", s)
	}
	if s, r := Score(Signals{ElapsedMS: 200, Webdriver: true}); s > 0.1 || r != "webdriver" {
		t.Fatalf("bot: %v %s", s, r)
	}
	if s, r := Score(Signals{ElapsedMS: 5000, Keys: 20, KeyVariance: 0, Languages: 1}); r != "robotic_typing" || s > 0.8 {
		t.Fatalf("robotic typing: %v %s", s, r)
	}
	h := sha256.Sum256([]byte("x"))
	_ = h
}

func TestSpikeAlert(t *testing.T) {
	ctx := context.Background()
	s, st, _ := newTest(t)
	alerts := 0
	s.Alert = func(context.Context, int, string) { alerts++ }
	now := time.Now()
	for i := 0; i < 60; i++ {
		st.events = append(st.events, Event{Outcome: "blocked", Place: PlaceSignup, At: now.Add(-time.Minute)})
	}
	s.checkSpike(ctx, PlaceSignup)
	s.checkSpike(ctx, PlaceSignup)
	if alerts != 1 {
		t.Fatalf("alerts: %d", alerts)
	}
	stats, err := s.Stats(ctx)
	if err != nil || len(stats.Rows) != 1 || stats.Rows[0].Blocked != 60 || stats.Hourly[23] != 60 {
		t.Fatalf("stats: %v %+v", err, stats.Rows)
	}
}

func TestPuzzles(t *testing.T) {
	for i := 0; i < 5; i++ {
		v, x, err := sliderPuzzle()
		if err != nil || x < 70 || x > sliderW-pieceSize || v.Piece == "" {
			t.Fatalf("slider: %v %d", err, x)
		}
		r, a, err := rotatePuzzle()
		if err != nil || a < 0 || a >= 360 || r.Image == "" {
			t.Fatalf("rotate: %v %d", err, a)
		}
	}
}
