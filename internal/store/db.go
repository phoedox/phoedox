// Package store is the persistence layer for Postgres and SQLite.
//
// Queries are written once with "?" placeholders and rebound per dialect.
// Dialect differences (JSON casts, ILIKE) are handled by small helpers.
// Migrations live in db/migrations and run with goose on startup.
package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // sqlite driver "sqlite"

	"github.com/phoedox/phoedox/db"
)

// Dialect identifies the SQL dialect.
type Dialect string

const (
	Postgres Dialect = "postgres"
	SQLite   Dialect = "sqlite"
)

// DefaultTenant is used in single-tenant mode.
const DefaultTenant = "default"

// ErrNotFound is returned when a row does not exist (or is not visible).
var ErrNotFound = errors.New("not found")

// ErrConflict is returned for unique constraint violations.
var ErrConflict = errors.New("already exists")

// DB wraps the connection pool and the dialect.
type DB struct {
	SQL     *sql.DB
	Dialect Dialect
	// MultiTenant sets the tenant of the context on the connection before
	// every statement, so Postgres row-level security filters all rows.
	MultiTenant bool
}

// Option changes how Open connects.
type Option func(*options)

type options struct{ multiTenant bool }

// WithMultiTenant enables the hosted mode (Postgres only): each statement
// runs with the tenant from the context (WithTenant), missing = no rows.
func WithMultiTenant() Option { return func(o *options) { o.multiTenant = true } }

// Open connects and runs all migrations.
func Open(ctx context.Context, dialect Dialect, dsn string, opts ...Option) (*DB, error) {
	return openUpTo(ctx, dialect, dsn, 0, opts...)
}

type tenantKey struct{}

// SystemTenant gives access to all tenants; only for system work
// (migrations, background jobs, housekeeping), never for requests.
const SystemTenant = "*"

// WithTenant sets the tenant for the store calls made with ctx.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenantID)
}

// System is ctx with access to all tenants.
func System(ctx context.Context) context.Context { return WithTenant(ctx, SystemTenant) }

// TenantOf returns the tenant of ctx (the default tenant if none is set).
func TenantOf(ctx context.Context) string {
	if t, ok := ctx.Value(tenantKey{}).(string); ok && t != "" && t != SystemTenant {
		return t
	}
	return DefaultTenant
}

// pgOpen opens a Postgres pool whose connections start with the given
// phoedox.tenant setting.
func pgOpen(dsn, tenant string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	opt := "-c phoedox.tenant=" + tenant
	if o := cfg.RuntimeParams["options"]; o != "" {
		opt = o + " " + opt
	}
	cfg.RuntimeParams["options"] = opt
	return stdlib.OpenDB(*cfg), nil
}

// openUpTo migrates to the given version (0 = latest); tests use it to
// check data migrations.
func openUpTo(ctx context.Context, dialect Dialect, dsn string, version int64, opts ...Option) (*DB, error) {
	var o options
	for _, f := range opts {
		f(&o)
	}
	var gooseDialect string
	var sqlDB, migDB *sql.DB
	var err error
	switch dialect {
	case Postgres:
		gooseDialect = "postgres"
		// Migrations see all tenants; the app pool sees the default tenant
		// (single tenant) or none until a statement sets one (hosted).
		if migDB, err = pgOpen(dsn, SystemTenant); err != nil {
			return nil, err
		}
		defer migDB.Close()
		tenant := DefaultTenant
		if o.multiTenant {
			tenant = ""
		}
		if sqlDB, err = pgOpen(dsn, tenant); err != nil {
			return nil, err
		}
	case SQLite:
		if o.multiTenant {
			return nil, fmt.Errorf("store: multi-tenant mode needs PostgreSQL")
		}
		gooseDialect = "sqlite3"
		if !strings.Contains(dsn, "?") {
			dsn += "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
		}
		if sqlDB, err = sql.Open("sqlite", dsn); err != nil {
			return nil, err
		}
		sqlDB.SetMaxOpenConns(1) // SQLite allows one writer; keeps behaviour predictable
		migDB = sqlDB
	default:
		return nil, fmt.Errorf("store: unknown dialect %q", dialect)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	migrations, err := fs.Sub(db.Migrations, "migrations/"+string(dialect))
	if err != nil {
		return nil, err
	}
	provider, err := goose.NewProvider(goose.Dialect(gooseDialect), migDB, migrations)
	if err != nil {
		return nil, fmt.Errorf("store: migrations: %w", err)
	}
	if version > 0 {
		_, err = provider.UpTo(ctx, version)
	} else {
		_, err = provider.Up(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	d := &DB{SQL: sqlDB, Dialect: dialect, MultiTenant: o.multiTenant}
	if err := d.ensureTenant(System(ctx)); err != nil {
		return nil, err
	}
	return d, nil
}

// Close closes the pool.
func (d *DB) Close() error { return d.SQL.Close() }

// ensureTenant creates the default tenant (single-tenant installations).
func (d *DB) ensureTenant(ctx context.Context) error {
	_, err := d.Exec(ctx, `INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?) ON CONFLICT (id) DO NOTHING`,
		DefaultTenant, "Default", Now())
	return err
}

// q is implemented by *sql.DB and *sql.Tx.
type q interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Tx is a transaction with the same helpers as DB.
type Tx struct {
	tx      *sql.Tx
	dialect Dialect
}

// conn returns where to run a statement: the pool, or in hosted mode a
// connection set to the context's tenant (release returns it).
func (d *DB) conn(ctx context.Context) (q, func(), error) {
	if !d.MultiTenant {
		return d.SQL, func() {}, nil
	}
	c, err := d.SQL.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	tenant, _ := ctx.Value(tenantKey{}).(string)
	if _, err := c.ExecContext(ctx, `SELECT set_config('phoedox.tenant', $1, false)`, tenant); err != nil {
		c.Close()
		return nil, nil, err
	}
	return c, func() { c.Close() }, nil
}

// InTx runs fn in a transaction and commits if it returns nil.
func (d *DB) InTx(ctx context.Context, fn func(*Tx) error) error {
	c, release, err := d.conn(ctx)
	if err != nil {
		return err
	}
	defer release()
	var tx *sql.Tx
	switch b := c.(type) {
	case *sql.Conn:
		tx, err = b.BeginTx(ctx, nil)
	default:
		tx, err = d.SQL.BeginTx(ctx, nil)
	}
	if err != nil {
		return err
	}
	if err := fn(&Tx{tx: tx, dialect: d.Dialect}); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	c, release, err := d.conn(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return exec(ctx, c, d.Dialect, query, args...)
}

func (d *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	c, release, err := d.conn(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := c.QueryContext(ctx, rebind(d.Dialect, query), args...)
	if err != nil {
		release()
		return nil, err
	}
	// The connection goes back to the pool once the rows are closed
	// (sql.Conn.Close waits for them).
	go release()
	return rows, nil
}

func (d *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	c, release, err := d.conn(ctx)
	if err != nil {
		// sql.Row cannot carry an error from outside: a cancelled query makes
		// Scan fail instead of reporting "no rows".
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		return d.SQL.QueryRowContext(cctx, "SELECT 1")
	}
	row := c.QueryRowContext(ctx, rebind(d.Dialect, query), args...)
	go release()
	return row
}
func (t *Tx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return exec(ctx, t.tx, t.dialect, query, args...)
}
func (t *Tx) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, rebind(t.dialect, query), args...)
}
func (t *Tx) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, rebind(t.dialect, query), args...)
}

func exec(ctx context.Context, c q, dialect Dialect, query string, args ...any) (sql.Result, error) {
	res, err := c.ExecContext(ctx, rebind(dialect, query), args...)
	if err != nil && isUniqueViolation(err) {
		return nil, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return res, err
}

func isUniqueViolation(err error) bool {
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed") || strings.Contains(s, "SQLSTATE 23505") || strings.Contains(s, "duplicate key value")
}

// rebind converts ? placeholders to $1, $2, ... for Postgres.
// Queries must not contain literal question marks.
func rebind(d Dialect, query string) string {
	if d != Postgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Now returns the current UTC time truncated to milliseconds, as stored in the DB.
func Now() Time { return Time{time.Now().UTC().Truncate(time.Millisecond)} }

// Time scans timestamps from both dialects (time.Time, RFC 3339 text).
type Time struct{ time.Time }

const timeLayout = "2006-01-02T15:04:05.000Z07:00"

// Scan implements sql.Scanner.
func (t *Time) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		t.Time = time.Time{}
	case time.Time:
		t.Time = v.UTC()
	case string:
		return t.parse(v)
	case []byte:
		return t.parse(string(v))
	default:
		return fmt.Errorf("store: cannot scan %T into Time", src)
	}
	return nil
}

func (t *Time) parse(s string) error {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
		if p, err := time.Parse(layout, s); err == nil {
			t.Time = p.UTC()
			return nil
		}
	}
	return fmt.Errorf("store: cannot parse time %q", s)
}

// MarshalJSON encodes an unset time as null instead of year 1.
func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return t.Time.MarshalJSON()
}

// Value implements driver.Valuer. Text works for TIMESTAMPTZ and SQLite TEXT.
func (t Time) Value() (driver.Value, error) {
	if t.IsZero() {
		return nil, nil
	}
	return t.UTC().Format(timeLayout), nil
}

// JSON scans JSON/JSONB columns from both dialects as raw bytes.
type JSON []byte

// Scan implements sql.Scanner.
func (j *JSON) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*j = JSON("null")
	case []byte:
		*j = append(JSON(nil), v...)
	case string:
		*j = JSON(v)
	default:
		return fmt.Errorf("store: cannot scan %T into JSON", src)
	}
	return nil
}

// Value implements driver.Valuer.
func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "null", nil
	}
	return string(j), nil
}

// MarshalJSON keeps the raw JSON when encoding responses.
func (j JSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON stores the raw JSON.
func (j *JSON) UnmarshalJSON(b []byte) error {
	*j = append(JSON(nil), b...)
	return nil
}

// textOf returns an expression that compares a JSON column as text.
func (d Dialect) textOf(col string) string {
	if d == Postgres {
		return col + "::text"
	}
	return col
}

// like returns a case-insensitive LIKE operator.
func (d Dialect) like() string {
	if d == Postgres {
		return "ILIKE"
	}
	return "LIKE" // SQLite LIKE is case-insensitive for ASCII
}
