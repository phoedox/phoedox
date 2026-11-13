package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// DocUpdate is one stored Yjs update of a document.
type DocUpdate struct {
	Seq  int64
	Data []byte
}

// DocUpdates returns the update log of a document in order.
func (d *DB) DocUpdates(ctx context.Context, documentID string) ([]DocUpdate, error) {
	rows, err := d.Query(ctx, `SELECT seq, data FROM doc_updates WHERE document_id = ? ORDER BY seq`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DocUpdate
	for rows.Next() {
		var u DocUpdate
		if err := rows.Scan(&u.Seq, &u.Data); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// AppendDocUpdate stores an update with the given sequence number (the
// realtime hub assigns them per document).
func (d *DB) AppendDocUpdate(ctx context.Context, documentID string, seq int64, data []byte) error {
	_, err := d.Exec(ctx, `INSERT INTO doc_updates (document_id, seq, tenant_id, data, created_at) VALUES (?, ?, ?, ?, ?)`,
		documentID, seq, DefaultTenant, data, Now())
	return err
}

// CompactDocUpdates replaces all updates up to seq with one snapshot.
func (d *DB) CompactDocUpdates(ctx context.Context, documentID string, upto int64, snapshot []byte) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM doc_updates WHERE document_id = ? AND seq <= ?`, documentID, upto); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO doc_updates (document_id, seq, tenant_id, data, created_at) VALUES (?, ?, ?, ?, ?)`,
			documentID, upto, DefaultTenant, snapshot, Now())
		return err
	})
}

// ClearDocUpdates drops the update log (after changes outside live editing).
func (d *DB) ClearDocUpdates(ctx context.Context, documentID string) error {
	_, err := d.Exec(ctx, `DELETE FROM doc_updates WHERE document_id = ?`, documentID)
	return err
}

// AddCoauthor records that userID edited the latest live version too (it
// saved the same content another person saved a moment before).
func (d *DB) AddCoauthor(ctx context.Context, documentID, userID string, window time.Duration) error {
	return d.InTx(ctx, func(tx *Tx) error {
		var ver int
		var actor, kind, co string
		var at Time
		err := tx.QueryRow(ctx, `SELECT v.version, v.actor, v.kind, v.created_at, v.coauthors FROM document_versions v JOIN documents doc ON doc.id = v.document_id
			WHERE v.document_id = ? AND v.version = doc.version`, documentID).Scan(&ver, &actor, &kind, &at, &co)
		if err != nil || kind != "live" || Now().Sub(at.Time) > window || ActorUserID(actor) == userID {
			return nil
		}
		for _, id := range strings.Split(co, ",") {
			if id == userID {
				return nil
			}
		}
		if co != "" {
			co += ","
		}
		_, err = tx.Exec(ctx, `UPDATE document_versions SET coauthors = ? WHERE document_id = ? AND version = ?`, co+userID, documentID, ver)
		return err
	})
}

// SaveLive stores content from the live editor. Saves of the same actor
// within window update the latest live version in place; otherwise a new
// version of kind "live" is created. It reports whether a version was added.
func (d *DB) SaveLive(ctx context.Context, doc Document, actor string, window time.Duration) (Document, bool, error) {
	now := Now()
	created := false
	err := d.InTx(ctx, func(tx *Tx) error {
		var ver int
		var vActor, kind string
		var at Time
		err := tx.QueryRow(ctx, `SELECT v.version, v.actor, v.kind, v.created_at FROM document_versions v JOIN documents doc ON doc.id = v.document_id
			WHERE v.document_id = ? AND v.version = doc.version AND doc.deleted_at IS NULL`, doc.ID).Scan(&ver, &vActor, &kind, &at)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if vActor == actor && kind == "live" && now.Sub(at.Time) < window {
			if _, err := tx.Exec(ctx, `UPDATE document_versions SET title = ?, content = ?, created_at = ? WHERE document_id = ? AND version = ?`,
				doc.Title, doc.Content, now, doc.ID, ver); err != nil {
				return err
			}
			doc.Version, doc.UpdatedAt = ver, now
			_, err = tx.Exec(ctx, `UPDATE documents SET title = ?, content = ?, updated_at = ? WHERE id = ?`, doc.Title, doc.Content, now, doc.ID)
			return err
		}
		updated, err := updateDoc(ctx, tx, doc, 0, actor, now)
		if err != nil {
			return err
		}
		doc = updated
		created = true
		_, err = tx.Exec(ctx, `UPDATE document_versions SET kind = 'live' WHERE document_id = ? AND version = ?`, doc.ID, doc.Version)
		return err
	})
	return doc, created, err
}
