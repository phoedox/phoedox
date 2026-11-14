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
	// WebsiteDomains serve the product website instead of the app, e.g.
	// phoedox.com,www.phoedox.com (PHOEDOX_WEBSITE_DOMAINS). WebsiteDir may
	// hold impressum.md and datenschutz.md (PHOEDOX_WEBSITE_DIR).
	WebsiteDomains []string
	WebsiteDir     string
	// DefaultLocale is used for emails to users without a language ("de").
	DefaultLocale string
	// RequestsPrivate lets request blocks reach private networks and
	// localhost (PHOEDOX_REQUESTS_PRIVATE, default true; false when hosted).
	RequestsPrivate bool
	// TrashDays is how long deleted items stay restorable (PHOEDOX_TRASH_DAYS,
	// default 30, 0 keeps them forever).
	TrashDays int
	// MasterKey encrypts secrets at rest (PHOEDOX_MASTER_KEY, 32 bytes
	// base64). Without it, <DataDir>/master.key is used or created.
	MasterKey string
	// MaxUploadMB limits the size of one uploaded file (PHOEDOX_MAX_UPLOAD_MB, default 100).
	MaxUploadMB int
	// S3 stores files in a bucket instead of <DataDir>/files when Endpoint is set.
	S3 S3
	// ClamAV is the clamd address for virus scans ("host:3310" or
	// "unix:/run/clamav/clamd.ctl", PHOEDOX_CLAMAV); empty disables scans.
	ClamAV string
	// OCR reads text from images and scanned PDFs with tesseract if
	// installed (PHOEDOX_OCR, default true); OCRLangs e.g. "deu+eng".
	OCR      bool
	OCRLangs string
	// Backups: automatic every BackupHours (PHOEDOX_BACKUP_HOURS, default 24,
	// 0 = off), the newest BackupKeep kept (PHOEDOX_BACKUP_KEEP, default 14),
	// stored in BackupDir (PHOEDOX_BACKUP_DIR, default <data>/backups) or the
	// S3 bucket (PHOEDOX_BACKUP_TARGET=s3). BackupFiles includes file contents
	// (PHOEDOX_BACKUP_FILES, default true unless files are on S3).
	BackupHours  int
	BackupKeep   int
	BackupDir    string
	BackupTarget string
	BackupFiles  bool
	// MetricsToken enables /metrics for Prometheus with this bearer token
	// (PHOEDOX_METRICS_TOKEN); empty disables it.
	MetricsToken string
	// Workers is the number of background jobs run in parallel
	// (PHOEDOX_WORKERS, default 2; 0 runs no jobs in this process).
	Workers int
	// Plugins runs WASM plugins (PHOEDOX_PLUGINS, default true).
	Plugins bool
	// StoreURL is the plugin store (PHOEDOX_STORE_URL, default
	// https://store.phoedox.com; "off" disables it).
	StoreURL string
	// AgentReleases is where agent binaries for self-updates come from
	// (PHOEDOX_AGENT_RELEASES, default the GitHub releases of phoedox-agent).
	AgentReleases string
	// PluginKeys are extra trusted signing keys "id:base64,…"
	// (PHOEDOX_PLUGIN_KEYS), added to the built-in store keys.
	PluginKeys string
	// PluginsUnsigned accepts uploaded plugins without a trusted signature
	// (PHOEDOX_PLUGINS_ALLOW_UNSIGNED, development only).
	PluginsUnsigned bool
	// OIDC enables sign-in with an external identity provider.
	OIDC OIDC
	// HAURL enables sign-in with Home Assistant (PHOEDOX_HA_URL): the address
	// browsers use. HAInternalURL is the one the server uses
	// (PHOEDOX_HA_INTERNAL_URL, default HAURL). HAAutoCreate creates accounts
	// on first sign-in (PHOEDOX_HA_AUTO_CREATE, default true).
	HAURL         string
	HAInternalURL string
	HAAutoCreate  bool
	// SMTP enables emails (invitations, password resets) when Host is set.
	SMTP SMTP
}

// S3 is an S3-compatible bucket for files (PHOEDOX_S3_*).
type S3 struct {
	Endpoint  string // host[:port]
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	Prefix    string
	Insecure  bool // plain HTTP, only for local test setups
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
	// GroupsClaim names the ID-token claim with the user's groups
	// (PHOEDOX_OIDC_GROUPS_CLAIM, default "groups"). Organization groups with
	// a matching "OIDC group" get the user as member at each sign-in.
	GroupsClaim string
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
		Domain:         os.Getenv("PHOEDOX_DOMAIN"),
		Listen:         getenv("PHOEDOX_LISTEN", ":8080"),
		DBDriver:       DBDriver(strings.ToLower(getenv("PHOEDOX_DB_DRIVER", string(DriverSQLite)))),
		DataDir:        getenv("PHOEDOX_DATA_DIR", "./data"),
		MultiTenant:    os.Getenv("PHOEDOX_MULTI_TENANT") == "true",
		SecureCookies:  os.Getenv("PHOEDOX_INSECURE_COOKIES") != "true",
		BehindProxy:    os.Getenv("PHOEDOX_BEHIND_PROXY") == "true",
		PublicURL:      strings.TrimRight(os.Getenv("PHOEDOX_PUBLIC_URL"), "/"),
		DefaultLocale:  getenv("PHOEDOX_DEFAULT_LOCALE", "de"),
		WebsiteDomains: splitList(strings.ToLower(os.Getenv("PHOEDOX_WEBSITE_DOMAINS"))),
		WebsiteDir:     os.Getenv("PHOEDOX_WEBSITE_DIR"),
		MasterKey:      os.Getenv("PHOEDOX_MASTER_KEY"),
		OIDC: OIDC{
			Issuer:       strings.TrimRight(os.Getenv("PHOEDOX_OIDC_ISSUER"), "/"),
			ClientID:     os.Getenv("PHOEDOX_OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("PHOEDOX_OIDC_CLIENT_SECRET"),
			Name:         getenv("PHOEDOX_OIDC_NAME", "SSO"),
			Scopes:       strings.Fields(getenv("PHOEDOX_OIDC_SCOPES", "openid email profile")),
			AutoCreate:   os.Getenv("PHOEDOX_OIDC_AUTO_CREATE") == "true",
			Domains:      splitList(os.Getenv("PHOEDOX_OIDC_DOMAINS")),
			TrustEmail:   os.Getenv("PHOEDOX_OIDC_TRUST_EMAIL") == "true",
			GroupsClaim:  getenv("PHOEDOX_OIDC_GROUPS_CLAIM", "groups"),
		},
		SMTP: SMTP{
			Host:     os.Getenv("PHOEDOX_SMTP_HOST"),
			User:     os.Getenv("PHOEDOX_SMTP_USER"),
			Password: os.Getenv("PHOEDOX_SMTP_PASSWORD"),
			From:     os.Getenv("PHOEDOX_SMTP_FROM"),
			TLS:      strings.ToLower(getenv("PHOEDOX_SMTP_TLS", "starttls")),
		},
	}
	c.RequestsPrivate = getenv("PHOEDOX_REQUESTS_PRIVATE", map[bool]string{true: "false", false: "true"}[c.MultiTenant]) == "true"
	td, err := strconv.Atoi(getenv("PHOEDOX_TRASH_DAYS", "30"))
	if err != nil || td < 0 {
		return c, fmt.Errorf("PHOEDOX_TRASH_DAYS: must be a number of days")
	}
	c.TrashDays = td
	mu, err := strconv.Atoi(getenv("PHOEDOX_MAX_UPLOAD_MB", "100"))
	if err != nil || mu < 1 {
		return c, fmt.Errorf("PHOEDOX_MAX_UPLOAD_MB: must be a positive number")
	}
	c.MaxUploadMB = mu
	c.ClamAV = os.Getenv("PHOEDOX_CLAMAV")
	c.MetricsToken = os.Getenv("PHOEDOX_METRICS_TOKEN")
	if c.BackupHours, err = strconv.Atoi(getenv("PHOEDOX_BACKUP_HOURS", "24")); err != nil || c.BackupHours < 0 {
		return c, fmt.Errorf("PHOEDOX_BACKUP_HOURS: must be 0 or more")
	}
	if c.BackupKeep, err = strconv.Atoi(getenv("PHOEDOX_BACKUP_KEEP", "14")); err != nil || c.BackupKeep < 0 {
		return c, fmt.Errorf("PHOEDOX_BACKUP_KEEP: must be 0 or more")
	}
	c.BackupDir = getenv("PHOEDOX_BACKUP_DIR", c.DataDir+"/backups")
	c.BackupTarget = getenv("PHOEDOX_BACKUP_TARGET", "local")
	if c.BackupTarget != "local" && c.BackupTarget != "s3" {
		return c, fmt.Errorf("PHOEDOX_BACKUP_TARGET: use local or s3")
	}
	c.BackupFiles = getenv("PHOEDOX_BACKUP_FILES", map[bool]string{true: "false", false: "true"}[c.S3.Endpoint != ""]) == "true"
	c.OCR = getenv("PHOEDOX_OCR", "true") == "true"
	c.OCRLangs = getenv("PHOEDOX_OCR_LANGS", "deu+eng")
	wk, err := strconv.Atoi(getenv("PHOEDOX_WORKERS", "2"))
	if err != nil || wk < 0 {
		return c, fmt.Errorf("PHOEDOX_WORKERS: must be 0 or more")
	}
	c.Workers = wk
	c.Plugins = getenv("PHOEDOX_PLUGINS", "true") == "true"
	c.StoreURL = getenv("PHOEDOX_STORE_URL", "https://store.phoedox.com")
	c.AgentReleases = strings.TrimRight(getenv("PHOEDOX_AGENT_RELEASES", "https://github.com/phoedox/phoedox-agent/releases"), "/")
	if c.StoreURL == "off" {
		c.StoreURL = ""
	}
	c.PluginKeys = os.Getenv("PHOEDOX_PLUGIN_KEYS")
	c.HAURL = strings.TrimRight(os.Getenv("PHOEDOX_HA_URL"), "/")
	c.HAInternalURL = strings.TrimRight(os.Getenv("PHOEDOX_HA_INTERNAL_URL"), "/")
	c.HAAutoCreate = getenv("PHOEDOX_HA_AUTO_CREATE", "true") == "true"
	c.PluginsUnsigned = os.Getenv("PHOEDOX_PLUGINS_ALLOW_UNSIGNED") == "true"
	c.S3 = S3{Endpoint: os.Getenv("PHOEDOX_S3_ENDPOINT"), Bucket: os.Getenv("PHOEDOX_S3_BUCKET"), Region: os.Getenv("PHOEDOX_S3_REGION"),
		AccessKey: os.Getenv("PHOEDOX_S3_ACCESS_KEY"), SecretKey: os.Getenv("PHOEDOX_S3_SECRET_KEY"), Prefix: os.Getenv("PHOEDOX_S3_PREFIX"),
		Insecure: os.Getenv("PHOEDOX_S3_INSECURE") == "true"}
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
