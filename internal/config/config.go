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
	// MasterKey encrypts secrets at rest (PHOEDOX_MASTER_KEY, 32 bytes
	// base64). Without it, <DataDir>/master.key is used or created.
	MasterKey string
	// SMTP enables emails (invitations, password resets) when Host is set.
	SMTP SMTP
}

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
		SMTP: SMTP{
			Host:     os.Getenv("PHOEDOX_SMTP_HOST"),
			User:     os.Getenv("PHOEDOX_SMTP_USER"),
			Password: os.Getenv("PHOEDOX_SMTP_PASSWORD"),
			From:     os.Getenv("PHOEDOX_SMTP_FROM"),
			TLS:      strings.ToLower(getenv("PHOEDOX_SMTP_TLS", "starttls")),
		},
	}
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

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
