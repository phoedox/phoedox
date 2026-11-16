package botcheck

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Store keeps the configuration, used one-time ids and the event log.
type Store interface {
	Setting(ctx context.Context, key, def string) (string, error)
	SetSetting(ctx context.Context, key, value, by string) error
	// MarkBotUsed records a one-time id; false means it was used before.
	MarkBotUsed(ctx context.Context, id string, until time.Time) (bool, error)
	AddBotEvent(ctx context.Context, e Event) error
	BotEvents(ctx context.Context, since time.Time, outcome string, limit int) ([]Event, error)
	PurgeBot(ctx context.Context, before time.Time) error
}

// Sealer encrypts challenges and tokens (the instance's master key box).
type Sealer interface {
	Seal(plain []byte, context string) (string, error)
	Open(sealed, context string) ([]byte, error)
}

// Mailer sends the one-time e-mail code.
type Mailer func(ctx context.Context, to, code, locale string) error

// Event is one outcome in the log (kept for 7 days).
type Event struct {
	ID      string    `json:"id"`
	Place   string    `json:"place"`
	Policy  string    `json:"policy"`
	Outcome string    `json:"outcome"` // passed | challenged | solved | email | blocked | failed
	Reason  string    `json:"reason,omitempty"`
	IPHash  string    `json:"ipHash,omitempty"`
	SolveMS int       `json:"solveMs,omitempty"`
	At      time.Time `json:"at"`
}

// Errors returned to callers of Check.
var (
	ErrRequired = errors.New("bot check required")
	ErrInvalid  = errors.New("bot check failed")
)

const settingKey = "botcheck.config"

// Service runs the checks.
type Service struct {
	Store  Store
	Sealer Sealer
	Mail   Mailer
	HTTP   *http.Client
	// Alert tells system admins about unusual spikes of blocked requests.
	Alert func(ctx context.Context, blocked int, place string)
	Now   func() time.Time

	mu        sync.Mutex
	busy      map[string][]time.Time // challenges per IP hash, for difficulty
	lastAlert time.Time
	salt      []byte
}

// New returns a service.
func New(st Store, sealer Sealer, mail Mailer) *Service {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	return &Service{Store: st, Sealer: sealer, Mail: mail, HTTP: &http.Client{Timeout: 10 * time.Second}, Now: time.Now, busy: map[string][]time.Time{}, salt: salt}
}

// Config loads the configuration (the default when none is stored).
func (s *Service) Config(ctx context.Context) Config {
	raw, _ := s.Store.Setting(ctx, settingKey, "")
	c := DefaultConfig()
	if raw != "" {
		var stored Config
		if json.Unmarshal([]byte(raw), &stored) == nil {
			c = stored
		}
	}
	c.normalize()
	return c
}

// SaveConfig validates and stores a configuration. Provider secrets that
// are empty keep their stored value; new ones are sealed.
func (s *Service) SaveConfig(ctx context.Context, c Config, by string) (Config, error) {
	old := s.Config(ctx)
	c.normalize()
	for name, k := range c.Providers {
		if k == nil {
			delete(c.Providers, name)
			continue
		}
		switch {
		case k.Secret != "":
			sealed, err := s.Sealer.Seal([]byte(k.Secret), "botcheck:"+name)
			if err != nil {
				return Config{}, err
			}
			k.Secret, k.HasSecret = sealed, true
		case old.Providers[name] != nil && old.Providers[name].HasSecret:
			k.Secret, k.HasSecret = old.Providers[name].Secret, true
		}
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	raw, _ := json.Marshal(c)
	if err := s.Store.SetSetting(ctx, settingKey, string(raw), by); err != nil {
		return Config{}, err
	}
	return Public(c), nil
}

// Public is a configuration without provider secrets.
func Public(c Config) Config {
	out := c
	out.Providers = map[string]*ProviderKeys{}
	for n, k := range c.Providers {
		cp := *k
		cp.Secret = ""
		out.Providers[n] = &cp
	}
	return out
}

// Off switches the protection off for every place (emergency).
func (s *Service) Off(ctx context.Context, by string) error {
	c := s.Config(ctx)
	for p := range c.Places {
		c.Places[p] = ""
	}
	for _, p := range Places {
		c.Places[p] = ""
	}
	raw, _ := json.Marshal(c)
	return s.Store.SetSetting(ctx, settingKey, string(raw), by)
}

// Enabled tells whether a place is protected.
func (s *Service) Enabled(ctx context.Context, place string) bool {
	_, ok := s.Config(ctx).ForPlace(place)
	return ok
}

// IPHash is a short, salted hash of an address (the salt changes with each
// start, so hashes cannot be linked across days of operation).
func (s *Service) IPHash(ip string) string {
	h := sha256.Sum256(append(s.salt, []byte(ip)...))
	return hex.EncodeToString(h[:6])
}

// --- sealed values ---------------------------------------------------------

// challenge is what the client gets sealed and sends back.
type challenge struct {
	ID         string    `json:"i"`
	Place      string    `json:"p"`
	Policy     string    `json:"o"`
	Prefix     string    `json:"x"`
	Difficulty int       `json:"d"`
	IP         string    `json:"h"`
	Issued     time.Time `json:"t"`
	Expires    time.Time `json:"e"`
	// Visible challenge after the invisible checks.
	Kind   string  `json:"k,omitempty"`
	Answer float64 `json:"a,omitempty"`
	Tries  int     `json:"n,omitempty"`
	// E-mail code.
	Email string `json:"m,omitempty"`
	Code  string `json:"c,omitempty"`
}

// pass is a sealed token that proves a passed check for a place.
type pass struct {
	ID      string    `json:"i"`
	Place   string    `json:"p"`
	Expires time.Time `json:"e"`
}

func (s *Service) seal(v any, kind string) (string, error) {
	raw, _ := json.Marshal(v)
	return s.Sealer.Seal(raw, "botcheck:"+kind)
}

func (s *Service) open(sealed, kind string, v any) error {
	raw, err := s.Sealer.Open(sealed, "botcheck:"+kind)
	if err != nil {
		return ErrInvalid
	}
	return json.Unmarshal(raw, v)
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// --- issuing ---------------------------------------------------------------

// Challenge is what a client needs to run the checks of a place.
type Challenge struct {
	Off        bool   `json:"off,omitempty"`
	Provider   string `json:"provider,omitempty"`
	SiteKey    string `json:"siteKey,omitempty"`
	Token      string `json:"challenge,omitempty"`
	Prefix     string `json:"prefix,omitempty"`
	Difficulty int    `json:"difficulty,omitempty"`
	MinMS      int    `json:"minMs,omitempty"`
	Email      bool   `json:"emailFallback,omitempty"`
}

const minFormMS = 1500

// Issue starts the checks of a place for a client.
func (s *Service) Issue(ctx context.Context, place, ipHash string) (Challenge, error) {
	c := s.Config(ctx)
	p, ok := c.ForPlace(place)
	if !ok {
		return Challenge{Off: true}, nil
	}
	now := s.Now()
	diff := p.Difficulty
	if diff == 0 {
		diff = 16
	}
	// Busy addresses get harder puzzles: +1 bit per 10 challenges in 10 minutes.
	diff = min(24, diff+s.busyCount(ipHash, now)/10)
	ch := challenge{ID: newID(), Place: place, Policy: p.ID, Prefix: newID(), Difficulty: diff, IP: ipHash, Issued: now, Expires: now.Add(15 * time.Minute)}
	tok, err := s.seal(ch, "challenge")
	if err != nil {
		return Challenge{}, err
	}
	out := Challenge{Provider: p.Provider, Token: tok, Prefix: ch.Prefix, Difficulty: diff, MinMS: minFormMS, Email: p.EmailFallback && s.Mail != nil}
	if p.Provider != ProviderOwn {
		if k := c.Providers[p.Provider]; k != nil {
			out.SiteKey = k.SiteKey
		}
	}
	return out, nil
}

func (s *Service) busyCount(ip string, now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var recent []time.Time
	for _, t := range s.busy[ip] {
		if now.Sub(t) < 10*time.Minute {
			recent = append(recent, t)
		}
	}
	s.busy[ip] = append(recent, now)
	if len(s.busy) > 50_000 {
		s.busy = map[string][]time.Time{}
	}
	return len(recent)
}

// --- invisible checks ------------------------------------------------------

// Signals are measured in the browser. Only aggregates are sent, never raw
// movements.
type Signals struct {
	ElapsedMS    int     `json:"elapsedMs"`
	Honeypot     bool    `json:"honeypot"`
	PointerMoves int     `json:"pointerMoves"`
	PointerDist  float64 `json:"pointerDist"`
	Keys         int     `json:"keys"`
	KeyVariance  float64 `json:"keyVariance"` // variance of key intervals (ms²)
	Touches      int     `json:"touches"`
	Focus        int     `json:"focus"`
	Webdriver    bool    `json:"webdriver"`
	Headless     bool    `json:"headless"`
	Plugins      int     `json:"plugins"`
	Languages    int     `json:"languages"`
	Behaviour    float64 `json:"behaviour"` // the browser's own score, 0..1
}

// Score turns the signals into a score between 0 (bot) and 1 (person) and
// names the strongest reason against.
func Score(sg Signals) (float64, string) {
	score, reason := 1.0, ""
	cut := func(by float64, why string) {
		score -= by
		if reason == "" {
			reason = why
		}
	}
	if sg.Honeypot {
		return 0, "honeypot"
	}
	if sg.Webdriver {
		cut(0.6, "webdriver")
	}
	if sg.Headless {
		cut(0.4, "headless")
	}
	if sg.ElapsedMS < minFormMS {
		cut(0.5, "too_fast")
	}
	human := sg.PointerMoves >= 3 || sg.Touches > 0 || sg.Keys >= 3
	if !human {
		cut(0.55, "no_interaction")
	}
	if sg.Keys >= 6 && sg.KeyVariance < 4 {
		cut(0.25, "robotic_typing") // perfectly even key intervals
	}
	if sg.Languages == 0 {
		cut(0.1, "no_languages")
	}
	if sg.Behaviour > 0 && sg.Behaviour < 0.3 {
		cut(0.2, "behaviour")
	}
	return math.Max(0, math.Min(1, score)), reason
}

// powOK checks the proof of work: sha256(prefix + nonce) starts with
// difficulty zero bits.
func powOK(prefix, nonce string, difficulty int) bool {
	if len(nonce) == 0 || len(nonce) > 32 {
		return false
	}
	h := sha256.Sum256([]byte(prefix + nonce))
	for i := 0; i < difficulty; i++ {
		if h[i/8]&(0x80>>(i%8)) != 0 {
			return false
		}
	}
	return true
}

// Result of a step: a pass token, a visible challenge, or blocked.
type Result struct {
	Token   string   `json:"token,omitempty"`
	Visible *Visible `json:"visible,omitempty"`
	Blocked bool     `json:"blocked,omitempty"`
	Email   bool     `json:"emailFallback,omitempty"`
	Retry   bool     `json:"retry,omitempty"`
}

// Visible is a puzzle to show.
type Visible struct {
	Kind   string `json:"kind"` // slider | rotate
	Token  string `json:"challenge"`
	Image  string `json:"image"`           // data URL
	Piece  string `json:"piece,omitempty"` // slider piece (data URL)
	PieceY int    `json:"pieceY,omitempty"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int    `json:"size,omitempty"`
}

// VerifyInput is the answer to the invisible checks.
type VerifyInput struct {
	Challenge string  `json:"challenge"`
	Nonce     string  `json:"nonce"`
	Signals   Signals `json:"signals"`
}

func (s *Service) loadChallenge(ctx context.Context, tok, ipHash string) (challenge, Policy, error) {
	var ch challenge
	if err := s.open(tok, "challenge", &ch); err != nil {
		return ch, Policy{}, err
	}
	if s.Now().After(ch.Expires) {
		return ch, Policy{}, ErrInvalid
	}
	p, ok := s.Config(ctx).Policy(ch.Policy)
	if !ok {
		return ch, Policy{}, ErrInvalid
	}
	return ch, p, nil
}

// Verify checks the invisible part and decides what comes next.
func (s *Service) Verify(ctx context.Context, in VerifyInput, ipHash string) (Result, error) {
	ch, p, err := s.loadChallenge(ctx, in.Challenge, ipHash)
	if err != nil {
		return Result{}, err
	}
	ms := int(s.Now().Sub(ch.Issued).Milliseconds())
	if !powOK(ch.Prefix, in.Nonce, ch.Difficulty) {
		s.log(ctx, ch, "failed", "proof_of_work", ipHash, ms)
		return Result{Blocked: true}, nil
	}
	if first, err := s.Store.MarkBotUsed(ctx, "c:"+ch.ID, ch.Expires); err != nil || !first {
		return Result{}, ErrInvalid
	}
	score, reason := Score(in.Signals)
	switch {
	case score >= p.PassAbove:
		s.log(ctx, ch, "passed", "", ipHash, ms)
		return s.passFor(ch)
	case score < p.BlockBelow:
		s.log(ctx, ch, "blocked", reason, ipHash, ms)
		s.checkSpike(ctx, ch.Place)
		return Result{Blocked: true, Email: p.EmailFallback && s.Mail != nil}, nil
	case p.Visible == "none":
		s.log(ctx, ch, "blocked", reason, ipHash, ms)
		return Result{Blocked: true, Email: p.EmailFallback && s.Mail != nil}, nil
	}
	s.log(ctx, ch, "challenged", reason, ipHash, ms)
	v, err := s.visibleFor(ch, p)
	if err != nil {
		return Result{}, err
	}
	return Result{Visible: v, Email: p.EmailFallback && s.Mail != nil}, nil
}

func (s *Service) passFor(ch challenge) (Result, error) {
	tok, err := s.seal(pass{ID: newID(), Place: ch.Place, Expires: s.Now().Add(10 * time.Minute)}, "pass")
	return Result{Token: tok}, err
}

func randInt(n int) int {
	v, _ := rand.Int(rand.Reader, big.NewInt(int64(n)))
	return int(v.Int64())
}

func (s *Service) visibleFor(ch challenge, p Policy) (*Visible, error) {
	kind := p.Visible
	if kind == "any" || kind == "" {
		kind = []string{"slider", "rotate"}[randInt(2)]
	}
	ch.Kind = kind
	var v Visible
	var err error
	if kind == "slider" {
		var x int
		v, x, err = sliderPuzzle()
		ch.Answer = float64(x)
	} else {
		var angle int
		v, angle, err = rotatePuzzle()
		ch.Answer = float64(angle)
	}
	if err != nil {
		return nil, err
	}
	ch.Expires = s.Now().Add(5 * time.Minute)
	v.Token, err = s.seal(ch, "visible")
	return &v, err
}

// SolveInput answers a visible challenge: the slider position (x) or the
// rotation in degrees.
type SolveInput struct {
	Challenge string  `json:"challenge"`
	Value     float64 `json:"value"`
}

// Solve checks a visible challenge; three wrong answers block.
func (s *Service) Solve(ctx context.Context, in SolveInput, ipHash string) (Result, error) {
	var ch challenge
	if err := s.open(in.Challenge, "visible", &ch); err != nil {
		return Result{}, err
	}
	if s.Now().After(ch.Expires) {
		return Result{}, ErrInvalid
	}
	p, ok := s.Config(ctx).Policy(ch.Policy)
	if !ok {
		return Result{}, ErrInvalid
	}
	ms := int(s.Now().Sub(ch.Issued).Milliseconds())
	if first, err := s.Store.MarkBotUsed(ctx, fmt.Sprintf("v:%s:%d", ch.ID, ch.Tries), ch.Expires); err != nil || !first {
		return Result{}, ErrInvalid
	}
	right := false
	switch ch.Kind {
	case "slider":
		right = math.Abs(in.Value-ch.Answer) <= 6
	case "rotate":
		d := math.Mod(math.Abs(in.Value-ch.Answer), 360)
		right = d <= 15 || d >= 345
	}
	if right {
		s.log(ctx, ch, "solved", ch.Kind, ipHash, ms)
		return s.passFor(ch)
	}
	if ch.Tries >= 2 {
		s.log(ctx, ch, "blocked", "puzzle_failed", ipHash, ms)
		s.checkSpike(ctx, ch.Place)
		return Result{Blocked: true, Email: p.EmailFallback && s.Mail != nil}, nil
	}
	ch.Tries++
	v, err := s.visibleFor(ch, Policy{Visible: ch.Kind})
	if err != nil {
		return Result{}, err
	}
	return Result{Visible: v, Retry: true, Email: p.EmailFallback && s.Mail != nil}, nil
}

// --- e-mail code -----------------------------------------------------------

// EmailInput asks for a code (Email) or checks one (Code with the sealed
// value from the first step).
type EmailInput struct {
	Challenge string `json:"challenge"`
	Email     string `json:"email,omitempty"`
	Locale    string `json:"locale,omitempty"`
	Code      string `json:"code,omitempty"`
}

// EmailCode sends a one-time code; the answer carries the sealed state.
func (s *Service) EmailCode(ctx context.Context, in EmailInput, ipHash string) (Result, error) {
	ch, p, err := s.loadChallenge(ctx, in.Challenge, ipHash)
	if err != nil {
		var v challenge
		if s.open(in.Challenge, "visible", &v) != nil {
			return Result{}, err
		}
		ch = v
		p, _ = s.Config(ctx).Policy(v.Policy)
	}
	if !p.EmailFallback || s.Mail == nil {
		return Result{}, ErrInvalid
	}
	email := strings.TrimSpace(strings.ToLower(in.Email))
	if !strings.Contains(email, "@") || len(email) > 254 {
		return Result{}, ErrInvalid
	}
	if first, err := s.Store.MarkBotUsed(ctx, "m:"+ch.ID, s.Now().Add(15*time.Minute)); err != nil || !first {
		return Result{}, ErrInvalid // one code per challenge
	}
	code := fmt.Sprintf("%06d", randInt(1_000_000))
	if err := s.Mail(ctx, email, code, in.Locale); err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256([]byte(ch.ID + code))
	ch.Email, ch.Code, ch.Kind = email, hex.EncodeToString(sum[:]), "email"
	ch.Expires = s.Now().Add(15 * time.Minute)
	tok, err := s.seal(ch, "email")
	return Result{Token: "", Visible: &Visible{Kind: "email", Token: tok}}, err
}

// EmailVerify checks a code from EmailCode.
func (s *Service) EmailVerify(ctx context.Context, in EmailInput, ipHash string) (Result, error) {
	var ch challenge
	if err := s.open(in.Challenge, "email", &ch); err != nil {
		return Result{}, err
	}
	if s.Now().After(ch.Expires) {
		return Result{}, ErrInvalid
	}
	if first, err := s.Store.MarkBotUsed(ctx, fmt.Sprintf("e:%s:%d", ch.ID, ch.Tries), ch.Expires); err != nil || !first {
		return Result{}, ErrInvalid
	}
	sum := sha256.Sum256([]byte(ch.ID + strings.TrimSpace(in.Code)))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(ch.Code)) == 1 {
		s.log(ctx, ch, "email", "", ipHash, int(s.Now().Sub(ch.Issued).Milliseconds()))
		return s.passFor(ch)
	}
	if ch.Tries >= 4 {
		s.log(ctx, ch, "blocked", "email_code", ipHash, 0)
		return Result{Blocked: true}, nil
	}
	ch.Tries++
	tok, err := s.seal(ch, "email")
	return Result{Retry: true, Visible: &Visible{Kind: "email", Token: tok}}, err
}

// --- tokens ----------------------------------------------------------------

// Check consumes a pass token for a place. It returns nil when the place is
// not protected.
func (s *Service) Check(ctx context.Context, place, token string) error {
	if !s.Enabled(ctx, place) {
		return nil
	}
	if token == "" {
		return ErrRequired
	}
	var p pass
	if err := s.open(token, "pass", &p); err != nil || p.Place != place || s.Now().After(p.Expires) {
		return ErrInvalid
	}
	first, err := s.Store.MarkBotUsed(ctx, "p:"+p.ID, p.Expires)
	if err != nil || !first {
		return ErrInvalid
	}
	return nil
}

func (s *Service) log(ctx context.Context, ch challenge, outcome, reason, ipHash string, ms int) {
	_ = s.Store.AddBotEvent(ctx, Event{ID: newID(), Place: ch.Place, Policy: ch.Policy, Outcome: outcome, Reason: reason, IPHash: ipHash, SolveMS: ms, At: s.Now()})
}
