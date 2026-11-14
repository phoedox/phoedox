// Package backup writes and restores complete backups of an instance in a
// database-independent format: a tar.gz with a manifest, one JSON-lines
// file per table and the file contents. A backup made on SQLite can be
// restored on Postgres and the other way round (moving an instance).
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/store"
)

// Format is the version of the backup layout.
const Format = 1

// Tables in the order they are restored (parents before children). The
// search index is rebuilt after a restore; unfinished uploads are skipped.
// A test checks that every table of the schema is listed or skipped.
var Tables = []string{
	"tenants", "tenant_domains", "users", "organizations", "org_members", "groups", "group_members",
	"projects", "project_members", "project_groups", "entities", "edges",
	"documents", "document_versions", "document_links", "document_grants", "favorites", "events",
	"sessions", "api_tokens", "suggestions", "account_links", "user_totp", "recovery_codes", "webauthn_credentials",
	"auth_challenges", "oauth_clients", "oauth_grants", "oauth_codes", "oauth_tokens", "user_identities",
	"doc_updates", "project_env", "instance_settings", "project_kinds",
	"files", "file_versions", "jobs", "share_links", "webhooks", "webhook_deliveries",
	"plugins", "project_plugins", "plugin_kv", "drafts", "document_translations", "tasks", "images", "publications", "agent_enrollments", "agents", "signing_keys", "agent_jobs", "publication_releases", "checks", "comments", "notifications", "notification_settings", "push_subscriptions", "project_git", "org_domains", "incidents", "incident_updates", "recent_views", "user_prefs", "secret_links", "page_rules", "read_confirmations", "maintenance_plans", "entity_meta", "kind_rules", "saved_queries", "doc_skills", "device_settings", "control_grants", "entity_readings",
}

// Skipped are tables that are not backed up.
var Skipped = []string{"goose_db_version", "sqlite_sequence", "search_index", "uploads"}

// Manifest describes a backup.
type Manifest struct {
	Format    int            `json:"format"`
	App       string         `json:"app"`       // server version
	Schema    int64          `json:"schema"`    // migration version
	Dialect   string         `json:"dialect"`   // where it was made
	CreatedAt time.Time      `json:"createdAt"` // UTC
	Rows      map[string]int `json:"rows"`
	Blobs     int            `json:"blobs"`
	WithFiles bool           `json:"withFiles"`
	// KeyID identifies the master key that sealed the secrets in the backup;
	// the key itself is never part of a backup.
	KeyID string `json:"keyId,omitempty"`
}

// Export writes a backup of all tenants to w. With files, the contents of
// all uploaded files and previews are included.
func Export(ctx context.Context, db *store.DB, blobs blob.Store, withFiles bool, app, keyID string, w io.Writer) (Manifest, error) {
	ctx = store.System(ctx)
	m := Manifest{Format: Format, App: app, Dialect: string(db.Dialect), CreatedAt: time.Now().UTC(), Rows: map[string]int{}, WithFiles: withFiles, KeyID: keyID}
	var err error
	if m.Schema, err = schemaVersion(ctx, db); err != nil {
		return m, err
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	tmp, err := os.CreateTemp("", "phx-backup-*")
	if err != nil {
		return m, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	for _, t := range Tables {
		if err := tmp.Truncate(0); err != nil {
			return m, err
		}
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return m, err
		}
		n, err := dumpTable(ctx, db, t, tmp)
		if err != nil {
			return m, fmt.Errorf("backup %s: %w", t, err)
		}
		m.Rows[t] = n
		if err := addFile(tw, "tables/"+t+".jsonl", tmp); err != nil {
			return m, err
		}
	}
	// Plugin packages are always included: without them a restored
	// instance could not run its plugins.
	if blobs != nil {
		hashes, err := allHashes(ctx, db, withFiles)
		if err != nil {
			return m, err
		}
		for _, h := range hashes {
			rc, err := blobs.Open(ctx, h)
			if errors.Is(err, blob.ErrNotFound) {
				continue
			}
			if err != nil {
				return m, err
			}
			size, err := rc.Seek(0, io.SeekEnd)
			if err == nil {
				_, err = rc.Seek(0, io.SeekStart)
			}
			if err == nil {
				err = tw.WriteHeader(&tar.Header{Name: "blobs/" + h, Mode: 0o600, Size: size, ModTime: m.CreatedAt})
			}
			if err == nil {
				_, err = io.Copy(tw, rc)
			}
			rc.Close()
			if err != nil {
				return m, err
			}
			m.Blobs++
		}
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(mb)), ModTime: m.CreatedAt}); err != nil {
		return m, err
	}
	if _, err := tw.Write(mb); err != nil {
		return m, err
	}
	if err := tw.Close(); err != nil {
		return m, err
	}
	return m, gz.Close()
}

func addFile(tw *tar.Writer, name string, f *os.File) error {
	size, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size, ModTime: time.Now()}); err != nil {
		return err
	}
	_, err = io.CopyN(tw, f, size)
	return err
}

func schemaVersion(ctx context.Context, db *store.DB) (int64, error) {
	var v int64
	err := db.QueryRow(ctx, `SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied = `+boolTrue(db)).Scan(&v)
	return v, err
}

func boolTrue(db *store.DB) string {
	if db.Dialect == store.Postgres {
		return "true"
	}
	return "1"
}

func allHashes(ctx context.Context, db *store.DB, withFiles bool) ([]string, error) {
	q := `SELECT sha256 FROM plugins`
	if withFiles {
		q += ` UNION SELECT sha256 FROM file_versions UNION SELECT sha256 FROM files UNION SELECT thumb_sha256 FROM files WHERE thumb_sha256 <> ''`
	}
	rows, err := db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// selfOrder sorts tables that reference their own rows so parents are
// restored before children (boards after their page, replies after the
// comment they answer).
var selfOrder = map[string]string{"documents": "created_at, id", "comments": "created_at, id"}

// dumpTable writes one JSON object per row; bytes become {"$b64": …},
// times RFC 3339 strings, JSON columns stay JSON.
func dumpTable(ctx context.Context, db *store.DB, table string, w io.Writer) (int, error) {
	query := `SELECT * FROM ` + table
	if order, ok := selfOrder[table]; ok {
		query += ` ORDER BY ` + order
	}
	rows, err := db.Query(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	types, _ := rows.ColumnTypes()
	enc := json.NewEncoder(w)
	n := 0
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			if c == "vec" { // generated (Postgres search index)
				continue
			}
			row[c] = encodeValue(vals[i], strings.ToUpper(types[i].DatabaseTypeName()))
		}
		if err := enc.Encode(row); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func encodeValue(v any, dbType string) any {
	switch x := v.(type) {
	case nil:
		return nil
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case []byte:
		if dbType == "JSON" || dbType == "JSONB" {
			if json.Valid(x) {
				return json.RawMessage(x)
			}
		}
		if dbType == "TEXT" || dbType == "VARCHAR" {
			return string(x)
		}
		return map[string]string{"$b64": base64.StdEncoding.EncodeToString(x)}
	case string:
		if dbType == "JSONB" || dbType == "JSON" {
			if json.Valid([]byte(x)) {
				return json.RawMessage(x)
			}
		}
		return x
	default:
		return x
	}
}

// ReadManifest reads only the manifest of a backup file.
func ReadManifest(path string) (Manifest, error) {
	var m Manifest
	err := walk(path, func(name string, r io.Reader) error {
		if name == "manifest.json" {
			return json.NewDecoder(r).Decode(&m)
		}
		return nil
	})
	if err == nil && m.Format == 0 {
		err = errors.New("backup: no manifest")
	}
	return m, err
}

func walk(path string, fn func(name string, r io.Reader) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("backup: not a backup file: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || strings.Contains(h.Name, "..") {
			continue
		}
		if err := fn(h.Name, tr); err != nil {
			return err
		}
	}
}

// ErrNewer is returned for backups of a newer schema than this server knows.
var ErrNewer = errors.New("backup: made by a newer Phoedox version")

// Restore replaces all data with the backup at path (a file, since it is read
// twice: manifest first). File contents are added to the blob store.
func Restore(ctx context.Context, db *store.DB, blobs blob.Store, tmpDir, path string) (Manifest, error) {
	ctx = store.System(ctx)
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return Manifest{}, err
	}
	m, err := ReadManifest(path)
	if err != nil {
		return m, err
	}
	if m.Format > Format {
		return m, ErrNewer
	}
	cur, err := schemaVersion(ctx, db)
	if err != nil {
		return m, err
	}
	if m.Schema > cur {
		return m, ErrNewer
	}
	colTypes := map[string]map[string]string{}
	for _, t := range Tables {
		if colTypes[t], err = columns(ctx, db, t); err != nil {
			return m, err
		}
	}
	// Blobs first (outside the transaction; extra contents do no harm).
	if blobs != nil {
		err = walk(path, func(name string, r io.Reader) error {
			h, ok := strings.CutPrefix(name, "blobs/")
			if !ok {
				return nil
			}
			p, hash, size, _, err := blob.Spool(tmpDir, r, 1<<40)
			if err != nil {
				return err
			}
			defer os.Remove(p)
			if hash != h {
				return fmt.Errorf("backup: content of %s is damaged", h)
			}
			return blobs.Put(ctx, hash, p, size)
		})
		if err != nil {
			return m, err
		}
	}
	err = db.InTx(ctx, func(tx *store.Tx) error {
		if err := clear(ctx, db, tx); err != nil {
			return err
		}
		data := map[string]string{}
		if err := walk(path, func(name string, r io.Reader) error {
			t, ok := strings.CutPrefix(name, "tables/")
			if !ok {
				return nil
			}
			t = strings.TrimSuffix(t, ".jsonl")
			f, err := os.CreateTemp(tmpDir, "table-*")
			if err != nil {
				return err
			}
			defer f.Close()
			if _, err := io.Copy(f, r); err != nil {
				return err
			}
			data[t] = f.Name()
			return nil
		}); err != nil {
			return err
		}
		defer func() {
			for _, p := range data {
				os.Remove(p)
			}
		}()
		for _, t := range Tables {
			p, ok := data[t]
			if !ok {
				continue
			}
			if err := loadTable(ctx, db, tx, t, colTypes[t], p); err != nil {
				return fmt.Errorf("restore %s: %w", t, err)
			}
		}
		if db.Dialect == store.Postgres {
			_, err := tx.Exec(ctx, `SELECT setval(pg_get_serial_sequence('events', 'id'), GREATEST(COALESCE((SELECT MAX(id) FROM events), 0), 1))`)
			return err
		}
		return nil
	})
	return m, err
}

func clear(ctx context.Context, db *store.DB, tx *store.Tx) error {
	if db.Dialect == store.Postgres {
		_, err := tx.Exec(ctx, `TRUNCATE `+strings.Join(Tables, ", ")+`, search_index, uploads CASCADE`)
		return err
	}
	for i := len(Tables) - 1; i >= 0; i-- {
		if _, err := tx.Exec(ctx, `DELETE FROM `+Tables[i]); err != nil {
			return err
		}
	}
	for _, t := range []string{"search_index", "uploads"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+t); err != nil {
			return err
		}
	}
	return nil
}

// columns returns the column types of a table in this database (lower case).
func columns(ctx context.Context, db *store.DB, table string) (map[string]string, error) {
	out := map[string]string{}
	var q string
	if db.Dialect == store.Postgres {
		q = `SELECT column_name, data_type FROM information_schema.columns WHERE table_name = ? AND table_schema = current_schema() AND is_generated = 'NEVER'`
	} else {
		q = `SELECT name, type FROM pragma_table_info(?)`
	}
	rows, err := db.Query(ctx, q, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n, t string
		if err := rows.Scan(&n, &t); err != nil {
			return nil, err
		}
		out[n] = strings.ToLower(t)
	}
	return out, rows.Err()
}

func loadTable(ctx context.Context, db *store.DB, tx *store.Tx, table string, cols map[string]string, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.UseNumber()
	for dec.More() {
		var row map[string]any
		if err := dec.Decode(&row); err != nil {
			return err
		}
		names := make([]string, 0, len(row))
		args := make([]any, 0, len(row))
		for c, v := range row {
			typ, ok := cols[c]
			if !ok {
				continue // column of an older schema that is gone
			}
			val, err := decodeValue(v, typ, c, db.Dialect)
			if err != nil {
				return fmt.Errorf("%s: %w", c, err)
			}
			names = append(names, c)
			args = append(args, val)
		}
		if len(names) == 0 {
			continue
		}
		q := `INSERT INTO ` + table + ` (` + strings.Join(names, ", ") + `) VALUES (` + strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", ") + `)`
		if _, err := tx.Exec(ctx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

// decodeValue converts a value from the backup to what the target column takes.
func decodeValue(v any, typ, col string, dialect store.Dialect) (any, error) {
	if m, ok := v.(map[string]any); ok {
		if s, ok := m["$b64"].(string); ok && len(m) == 1 {
			return base64.StdEncoding.DecodeString(s)
		}
	}
	switch {
	case v == nil:
		return nil, nil
	case typ == "boolean":
		switch x := v.(type) {
		case bool:
			return x, nil
		case json.Number:
			return x.String() != "0", nil
		}
	case typ == "jsonb" || typ == "json":
		if s, ok := v.(string); ok {
			return s, nil
		}
		b, err := json.Marshal(v)
		return string(b), err
	case strings.Contains(typ, "timestamp"):
		return v, nil
	}
	switch x := v.(type) {
	case json.Number:
		if i, err := strconv.ParseInt(x.String(), 10, 64); err == nil {
			return i, nil
		}
		return x.Float64()
	case bool:
		if dialect == store.SQLite {
			if x {
				return int64(1), nil
			}
			return int64(0), nil
		}
		return x, nil
	case string:
		// SQLite compares times as text: keep the one layout the server writes.
		if dialect == store.SQLite && (strings.HasSuffix(col, "_at") || col == "expires") {
			if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
				return store.Time{Time: t}.Value()
			}
		}
		return x, nil
	case map[string]any, []any:
		b, err := json.Marshal(x)
		return string(b), err
	}
	return v, nil
}

// Name is the file name of a backup made at t.
// Milliseconds keep two backups of the same second apart (e.g. the one made
// right before a restore).
func Name(t time.Time) string { return "phoedox-" + t.UTC().Format("20060102-150405.000") + ".tar.gz" }

// IsName reports whether a file name looks like a backup (no paths).
func IsName(s string) bool {
	return strings.HasPrefix(s, "phoedox-") && strings.HasSuffix(s, ".tar.gz") && filepath.Base(s) == s && !strings.ContainsAny(s, `/\`)
}
