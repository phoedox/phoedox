// Package config loads the server configuration from environment variables.
package config

import (
	"fmt"
	"os"
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
	}
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
