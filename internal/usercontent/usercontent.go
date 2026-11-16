// Package usercontent serves apps and other user content on their own
// domain (e.g. phoedoxusercontent.com), one subdomain per app, so a
// program written by a person or the AI never runs on the origin of the
// Phoedox instance. Phoedox gets the wildcard certificate itself through
// ACME DNS-01 with the DNS provider the system admin chose.
package usercontent

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/cloudflare"
	"github.com/libdns/hetzner"
	"github.com/libdns/netcup"
)

// Config is what the system admin sets.
type Config struct {
	Domain   string `json:"domain"`   // e.g. phoedoxusercontent.com
	Provider string `json:"provider"` // hetzner | netcup | cloudflare
	Email    string `json:"email"`    // ACME account (expiry notices)
	Staging  bool   `json:"staging,omitempty"`
	// Credentials of the DNS provider (stored encrypted).
	Token          string `json:"token,omitempty"`          // Hetzner, Cloudflare
	CustomerNumber string `json:"customerNumber,omitempty"` // netcup
	APIKey         string `json:"apiKey,omitempty"`         // netcup
	APIPassword    string `json:"apiPassword,omitempty"`    // netcup
}

var domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// Validate checks the configuration (credentials only when required).
func (c *Config) Validate() error {
	c.Domain = strings.ToLower(strings.TrimSpace(c.Domain))
	if !domainRe.MatchString(c.Domain) {
		return errors.New("domain")
	}
	switch c.Provider {
	case "hetzner", "cloudflare":
		if c.Token == "" {
			return errors.New("token")
		}
	case "netcup":
		if c.CustomerNumber == "" || c.APIKey == "" || c.APIPassword == "" {
			return errors.New("netcup")
		}
	default:
		return errors.New("provider")
	}
	if !strings.Contains(c.Email, "@") {
		return errors.New("email")
	}
	return nil
}

func (c Config) dns() certmagic.DNSProvider {
	switch c.Provider {
	case "hetzner":
		return &hetzner.Provider{AuthAPIToken: c.Token}
	case "cloudflare":
		return &cloudflare.Provider{APIToken: c.Token}
	case "netcup":
		return &netcup.Provider{CustomerNumber: c.CustomerNumber, APIKey: c.APIKey, APIPassword: c.APIPassword}
	}
	return nil
}

// Status is the state of the certificate.
type Status struct {
	State   string    `json:"state"` // off | waiting | ready | failed
	Error   string    `json:"error,omitempty"`
	Expires time.Time `json:"expires,omitempty"`
	Since   time.Time `json:"since,omitempty"`
}

// Server is the HTTPS listener of the user content domain.
type Server struct {
	// Handler serves requests on <label>.<domain>.
	Handler http.Handler
	// Dir keeps the ACME account and certificates.
	Dir string

	mu     sync.Mutex
	cfg    Config
	magic  *certmagic.Config
	cancel context.CancelFunc
	status Status
}

// Domain is the configured domain ("" = off).
func (s *Server) Domain() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.magic == nil {
		return ""
	}
	return s.cfg.Domain
}

// Status reports the certificate state.
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Server) setStatus(st Status) {
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
}

// Apply starts (or restarts) managing the wildcard certificate; an empty
// domain switches the user content domain off.
func (s *Server) Apply(cfg Config) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.magic = nil
	s.cfg = cfg
	s.mu.Unlock()
	if cfg.Domain == "" {
		s.setStatus(Status{State: "off"})
		return nil
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	storage := &certmagic.FileStorage{Path: filepath.Join(s.Dir, "certs")}
	var magic *certmagic.Config
	cache := certmagic.NewCache(certmagic.CacheOptions{GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) { return magic, nil }})
	magic = certmagic.New(cache, certmagic.Config{
		Storage: storage,
		OnEvent: func(_ context.Context, event string, data map[string]any) error {
			switch event {
			case "cert_obtained", "cached_managed_cert":
				st := Status{State: "ready", Since: time.Now()}
				if exp, ok := data["expiration"].(time.Time); ok {
					st.Expires = exp
				}
				s.setStatus(st)
			case "cert_failed":
				msg := fmt.Sprint(data["error"])
				slog.Warn("user content certificate failed", "domain", cfg.Domain, "err", msg)
				s.setStatus(Status{State: "failed", Error: msg, Since: time.Now()})
			}
			return nil
		},
	})
	issuer := certmagic.NewACMEIssuer(magic, certmagic.ACMEIssuer{
		Email: cfg.Email, Agreed: true, DisableHTTPChallenge: true, DisableTLSALPNChallenge: true,
		DNS01Solver: &certmagic.DNS01Solver{DNSManager: certmagic.DNSManager{
			DNSProvider: cfg.dns(),
			// netcup takes 10–15 minutes until changes show.
			PropagationTimeout: 30 * time.Minute,
		}},
	})
	if cfg.Staging {
		issuer.CA = certmagic.LetsEncryptStagingCA
	}
	magic.Issuers = []certmagic.Issuer{issuer}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.magic, s.cancel = magic, cancel
	s.status = Status{State: "waiting", Since: time.Now()}
	s.mu.Unlock()
	return magic.ManageAsync(ctx, []string{"*." + cfg.Domain, cfg.Domain})
}

// Serve listens on addr (TLS) until ctx ends.
func (s *Server) Serve(ctx context.Context, addr string) error {
	tc := &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
		GetCertificate: func(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
			s.mu.Lock()
			magic, dom := s.magic, s.cfg.Domain
			s.mu.Unlock()
			name := strings.ToLower(h.ServerName)
			if magic == nil || (name != dom && !strings.HasSuffix(name, "."+dom)) {
				return nil, errors.New("unknown host")
			}
			return magic.GetCertificate(h)
		},
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.hosts(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	err = srv.Serve(tls.NewListener(ln, tc))
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// hosts lets only <label>.<domain> through and puts the label in the
// request header X-Phoedox-Host-Label.
func (s *Server) hosts() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		dom := s.Domain()
		label, ok := strings.CutSuffix(host, "."+dom)
		if dom == "" || !ok || label == "" || strings.Contains(label, ".") {
			http.NotFound(w, r)
			return
		}
		r.Header.Set("X-Phoedox-Host-Label", label)
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		s.Handler.ServeHTTP(w, r)
	})
}

// Label is the subdomain label of an app (document IDs are case-insensitive
// ULIDs).
func Label(docID string) string { return strings.ToLower(docID) }
