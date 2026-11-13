// Package config loads the server configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DBDriver selects the storage backend.
type DBDriver string

const (
	DriverSQLite   DBDriver = "sqlite"
	DriverPostgres DBDriver = "postgres"
)

// Config is the complete runtime configuration of a Phoedox instance.
type Config struct {
	// Domain is the public app domain, e.g. app.phoedox.com (PHOEDOX_DOMAIN).
	Domain string
	// Listen is the HTTP listen address (PHOEDOX_LISTEN), default ":8080".
	Listen string
	// DBDriver is "sqlite" (default) or "postgres" (PHOEDOX_DB_DRIVER).
	DBDriver DBDriver
	// DBURL is the DSN or file path (PHOEDOX_DB_URL).
	DBURL string
	// DataDir holds files, the SQLite database and caches (PHOEDOX_DATA_DIR).
	DataDir string
	// MultiTenant enables the hosted mode with row-level security (PHOEDOX_MULTI_TENANT).
	MultiTenant bool
	// SecureCookies sets the Secure flag on cookies. Default true; set
	// PHOEDOX_INSECURE_COOKIES=true only for local HTTP development.
	SecureCookies bool
	// BehindProxy trusts X-Forwarded-For / X-Real-IP for the client address
	// (PHOEDOX_BEHIND_PROXY). Only enable it when a reverse proxy sets them.
	BehindProxy bool
	// PublicURL is the external base URL for links in emails, e.g.
	// https://docs.example.org (PHOEDOX_PUBLIC_URL). Derived from the request
	// when empty.
	PublicURL string
	// DefaultLocale is used for emails to users without a language ("de").
	DefaultLocale string
	// TrashDays is how long deleted items stay restorable (PHOEDOX_TRASH_DAYS,
	// default 30, 0 keeps them forever).
	TrashDays int
	// MasterKey encrypts secrets at rest (PHOEDOX_MASTER_KEY, 32 bytes
	// base64). Without it, <DataDir>/master.key is used or created.
	MasterKey string
	// OIDC enables sign-in with an external identity provider.
	OIDC OIDC
	// SMTP enables emails (invitations, password resets) when Host is set.
	SMTP SMTP
}

// OIDC is an OpenID Connect provider (PHOEDOX_OIDC_*), e.g. Authentik,
// Keycloak, Entra ID or Google.
type OIDC struct {
	Issuer       string   // PHOEDOX_OIDC_ISSUER
	ClientID     string   // PHOEDOX_OIDC_CLIENT_ID
	ClientSecret string   // PHOEDOX_OIDC_CLIENT_SECRET
	Name         string   // PHOEDOX_OIDC_NAME, button label (default "SSO")
	Scopes       []string // PHOEDOX_OIDC_SCOPES, default "openid email profile"
	// AutoCreate creates accounts on first sign-in (PHOEDOX_OIDC_AUTO_CREATE).
	// Otherwise only existing (e.g. invited) users can sign in.
	AutoCreate bool
	// Domains limits auto-created accounts to these email domains
	// (PHOEDOX_OIDC_DOMAINS, comma separated).
	Domains []string
	// TrustEmail treats emails as verified even without email_verified
	// (PHOEDOX_OIDC_TRUST_EMAIL), for providers like Entra ID that omit it.
	TrustEmail bool
}

// Enabled reports whether OIDC is configured.
func (o OIDC) Enabled() bool { return o.Issuer != "" && o.ClientID != "" }

// SMTP is the outgoing mail server (PHOEDOX_SMTP_*).
type SMTP struct {
	Host     string // PHOEDOX_SMTP_HOST
	Port     int    // PHOEDOX_SMTP_PORT, default 587 (465 with TLS=tls)
	User     string // PHOEDOX_SMTP_USER
	Password string // PHOEDOX_SMTP_PASSWORD
	From     string // PHOEDOX_SMTP_FROM, e.g. "Phoedox <noreply@example.org>"
	TLS      string // PHOEDOX_SMTP_TLS: starttls (default), tls or none
}

// FromEnv reads the configuration and applies defaults.
func FromEnv() (Config, error) {
	c := Config{
		Domain:        os.Getenv("PHOEDOX_DOMAIN"),
		Listen:        getenv("PHOEDOX_LISTEN", ":8080"),
		DBDriver:      DBDriver(strings.ToLower(getenv("PHOEDOX_DB_DRIVER", string(DriverSQLite)))),
		DataDir:       getenv("PHOEDOX_DATA_DIR", "./data"),
		MultiTenant:   os.Getenv("PHOEDOX_MULTI_TENANT") == "true",
		SecureCookies: os.Getenv("PHOEDOX_INSECURE_COOKIES") != "true",
		BehindProxy:   os.Getenv("PHOEDOX_BEHIND_PROXY") == "true",
		PublicURL:     strings.TrimRight(os.Getenv("PHOEDOX_PUBLIC_URL"), "/"),
		DefaultLocale: getenv("PHOEDOX_DEFAULT_LOCALE", "de"),
		MasterKey:     os.Getenv("PHOEDOX_MASTER_KEY"),
		OIDC: OIDC{
			Issuer:       strings.TrimRight(os.Getenv("PHOEDOX_OIDC_ISSUER"), "/"),
			ClientID:     os.Getenv("PHOEDOX_OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("PHOEDOX_OIDC_CLIENT_SECRET"),
			Name:         getenv("PHOEDOX_OIDC_NAME", "SSO"),
			Scopes:       strings.Fields(getenv("PHOEDOX_OIDC_SCOPES", "openid email profile")),
			AutoCreate:   os.Getenv("PHOEDOX_OIDC_AUTO_CREATE") == "true",
			Domains:      splitList(os.Getenv("PHOEDOX_OIDC_DOMAINS")),
			TrustEmail:   os.Getenv("PHOEDOX_OIDC_TRUST_EMAIL") == "true",
		},
		SMTP: SMTP{
			Host:     os.Getenv("PHOEDOX_SMTP_HOST"),
			User:     os.Getenv("PHOEDOX_SMTP_USER"),
			Password: os.Getenv("PHOEDOX_SMTP_PASSWORD"),
			From:     os.Getenv("PHOEDOX_SMTP_FROM"),
			TLS:      strings.ToLower(getenv("PHOEDOX_SMTP_TLS", "starttls")),
		},
	}
	td, err := strconv.Atoi(getenv("PHOEDOX_TRASH_DAYS", "30"))
	if err != nil || td < 0 {
		return c, fmt.Errorf("PHOEDOX_TRASH_DAYS: must be a number of days")
	}
	c.TrashDays = td
	port := "587"
	if c.SMTP.TLS == "tls" {
		port = "465"
	}
	p, err := strconv.Atoi(getenv("PHOEDOX_SMTP_PORT", port))
	if err != nil {
		return c, fmt.Errorf("PHOEDOX_SMTP_PORT: %v", err)
	}
	c.SMTP.Port = p
	c.DBURL = getenv("PHOEDOX_DB_URL", c.DataDir+"/phoedox.db")
	return c, c.Validate()
}

// Validate checks combinations that cannot work.
func (c Config) Validate() error {
	switch c.DBDriver {
	case DriverSQLite, DriverPostgres:
	default:
		return fmt.Errorf("PHOEDOX_DB_DRIVER: unknown driver %q (use sqlite or postgres)", c.DBDriver)
	}
	if c.SMTP.Host != "" {
		switch c.SMTP.TLS {
		case "starttls", "tls", "none":
		default:
			return fmt.Errorf("PHOEDOX_SMTP_TLS: use starttls, tls or none")
		}
		if c.SMTP.From == "" {
			return fmt.Errorf("PHOEDOX_SMTP_FROM is required when PHOEDOX_SMTP_HOST is set")
		}
	}
	if c.PublicURL != "" && !strings.HasPrefix(c.PublicURL, "https://") && !strings.HasPrefix(c.PublicURL, "http://") {
		return fmt.Errorf("PHOEDOX_PUBLIC_URL must start with https:// or http://")
	}
	if c.MultiTenant && c.DBDriver != DriverPostgres {
		return fmt.Errorf("PHOEDOX_MULTI_TENANT requires PHOEDOX_DB_DRIVER=postgres")
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
