package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// GetVersion returns one stored version with content.
func (d *DB) GetVersion(ctx context.Context, documentID string, version int) (Version, error) {
	v := Version{Version: version}
	err := d.QueryRow(ctx, `SELECT title, actor, created_at, content FROM document_versions WHERE document_id = ? AND version = ?`, documentID, version).
		Scan(&v.Title, &v.Actor, &v.CreatedAt, &v.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

// UserNames maps user ids to display names.
func (d *DB) UserNames(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := d.Query(ctx, `SELECT id, name FROM users WHERE id IN (`+placeholders(len(ids))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// ActorUserID extracts the user id from an actor string
// (user:<id>, mcp:<id>:<token>, token:<id>:<token>).
func ActorUserID(actor string) string {
	parts := strings.Split(actor, ":")
	if len(parts) >= 2 && (parts[0] == "user" || parts[0] == "mcp" || parts[0] == "token") {
		return parts[1]
	}
	return ""
}

// TrashItem is a deleted document or entity.
type TrashItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // document | entity
	Type      string `json:"type"` // document type or entity kind
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	DeletedAt Time   `json:"deletedAt"`
}

// Trash lists deleted documents and entities of a project, newest first.
func (d *DB) Trash(ctx context.Context, projectID string) ([]TrashItem, error) {
	rows, err := d.Query(ctx, `SELECT id, 'document', doc_type, slug, title, deleted_at FROM documents WHERE project_id = ? AND deleted_at IS NOT NULL
		UNION ALL SELECT id, 'entity', kind, slug, name, deleted_at FROM entities WHERE project_id = ? AND deleted_at IS NOT NULL
		ORDER BY 6 DESC, 5`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrashItem{}
	for rows.Next() {
		var it TrashItem
		if err := rows.Scan(&it.ID, &it.Kind, &it.Type, &it.Slug, &it.Title, &it.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// RestoreDocument takes a document out of the trash. ErrConflict if its
// slug is used by another document meanwhile.
func (d *DB) RestoreDocument(ctx context.Context, projectID, id string) (Document, error) {
	res, err := d.Exec(ctx, `UPDATE documents SET deleted_at = NULL WHERE id = ? AND project_id = ? AND deleted_at IS NOT NULL`, id, projectID)
	if err := affected(res, err); err != nil {
		return Document{}, err
	}
	return d.GetDocument(ctx, projectID, id)
}

// RestoreEntity takes an entity out of the trash together with the edges
// deleted with it (if their other end exists).
func (d *DB) RestoreEntity(ctx context.Context, projectID, id string) (Entity, error) {
	err := d.InTx(ctx, func(tx *Tx) error {
		var deletedAt Time
		err := tx.QueryRow(ctx, `SELECT deleted_at FROM entities WHERE id = ? AND project_id = ? AND deleted_at IS NOT NULL`, id, projectID).Scan(&deletedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE entities SET deleted_at = NULL WHERE id = ?`, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE edges SET deleted_at = NULL WHERE deleted_at = ? AND (from_id = ? OR to_id = ?)
			AND NOT EXISTS (SELECT 1 FROM entities e WHERE (e.id = edges.from_id OR e.id = edges.to_id) AND e.deleted_at IS NOT NULL)`, deletedAt, id, id)
		return err
	})
	if err != nil {
		return Entity{}, err
	}
	return d.GetEntity(ctx, projectID, id)
}

// Purge deletes a trashed document or entity for good.
func (d *DB) Purge(ctx context.Context, projectID, id string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		res, err := tx.Exec(ctx, `DELETE FROM documents WHERE id = ? AND project_id = ? AND deleted_at IS NOT NULL`, id, projectID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			res, err = tx.Exec(ctx, `DELETE FROM entities WHERE id = ? AND project_id = ? AND deleted_at IS NOT NULL`, id, projectID)
			if err := affected(res, err); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM search_index WHERE ref_id = ?`, id)
		return err
	})
}

// PurgeBefore deletes everything trashed before the given time and
// returns how many documents and entities were removed.
func (d *DB) PurgeBefore(ctx context.Context, before Time) (int, error) {
	total := 0
	err := d.InTx(ctx, func(tx *Tx) error {
		for _, q := range []string{
			`DELETE FROM documents WHERE deleted_at IS NOT NULL AND deleted_at < ?`,
			`DELETE FROM edges WHERE deleted_at IS NOT NULL AND deleted_at < ?`,
			`DELETE FROM entities WHERE deleted_at IS NOT NULL AND deleted_at < ?`,
		} {
			res, err := tx.Exec(ctx, q, before)
			if err != nil {
				return err
			}
			if !strings.Contains(q, "edges") {
				n, _ := res.RowsAffected()
				total += int(n)
			}
		}
		return nil
	})
	return total, err
}

// Favorite marks a document for the whole project (Personal false) or for
// one user.
type Favorite struct {
	DocumentID string `json:"documentId"`
	Personal   bool   `json:"personal"`
	Position   int    `json:"position"`
}

// Favorites returns project favorites and the user's own favorites.
func (d *DB) Favorites(ctx context.Context, projectID, userID string) ([]Favorite, error) {
	rows, err := d.Query(ctx, `SELECT f.document_id, f.user_id IS NOT NULL, f.position FROM favorites f JOIN documents doc ON doc.id = f.document_id
		WHERE f.project_id = ? AND (f.user_id IS NULL OR f.user_id = ?) AND doc.deleted_at IS NULL
		ORDER BY f.user_id IS NOT NULL, f.position, f.created_at`, projectID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Favorite{}
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.DocumentID, &f.Personal, &f.Position); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func favUser(userID string) (string, []any) {
	if userID == "" {
		return `user_id IS NULL`, nil
	}
	return `user_id = ?`, []any{userID}
}

// AddFavorite adds a favorite at the end (no-op if it exists).
// userID "" adds a project favorite.
func (d *DB) AddFavorite(ctx context.Context, id, projectID, documentID, userID string) error {
	cond, uargs := favUser(userID)
	return d.InTx(ctx, func(tx *Tx) error {
		var n int
		args := append([]any{projectID, documentID}, uargs...)
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM favorites WHERE project_id = ? AND document_id = ? AND `+cond, args...).Scan(&n); err != nil || n > 0 {
			return err
		}
		var pos int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM favorites WHERE project_id = ? AND `+cond, append([]any{projectID}, uargs...)...).Scan(&pos); err != nil {
			return err
		}
		var user any
		if userID != "" {
			user = userID
		}
		_, err := tx.Exec(ctx, `INSERT INTO favorites (id, tenant_id, project_id, document_id, user_id, position, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, DefaultTenant, projectID, documentID, user, pos, Now())
		return err
	})
}

// RemoveFavorite removes a favorite (no error if it did not exist).
func (d *DB) RemoveFavorite(ctx context.Context, projectID, documentID, userID string) error {
	cond, uargs := favUser(userID)
	_, err := d.Exec(ctx, `DELETE FROM favorites WHERE project_id = ? AND document_id = ? AND `+cond, append([]any{projectID, documentID}, uargs...)...)
	return err
}

// ReorderFavorites sets positions in the given order.
func (d *DB) ReorderFavorites(ctx context.Context, projectID, userID string, documentIDs []string) error {
	cond, uargs := favUser(userID)
	return d.InTx(ctx, func(tx *Tx) error {
		for i, doc := range documentIDs {
			if _, err := tx.Exec(ctx, `UPDATE favorites SET position = ? WHERE project_id = ? AND document_id = ? AND `+cond, append([]any{i + 1, projectID, doc}, uargs...)...); err != nil {
				return err
			}
		}
		return nil
	})
}
