package store

import (
	"context"
	"database/sql"
	"errors"
)

// BlockState is what one reader did in a block of a page (steps, poll, quiz).
type BlockState struct {
	DocumentID string `json:"-"`
	Key        string `json:"key"`
	UserID     string `json:"-"`
	Data       string `json:"-"`
	UpdatedAt  Time   `json:"updatedAt"`
}

// BlockState reads one reader's state of a block; ErrNotFound if there is none.
func (d *DB) BlockState(ctx context.Context, docID, key, userID string) (BlockState, error) {
	var b BlockState
	err := d.QueryRow(ctx, `SELECT document_id, block_key, user_id, data, updated_at FROM block_states WHERE document_id = ? AND block_key = ? AND user_id = ?`,
		docID, key, userID).Scan(&b.DocumentID, &b.Key, &b.UserID, &b.Data, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// BlockStates lists all readers' states of a block (for counting votes).
func (d *DB) BlockStates(ctx context.Context, docID, key string) ([]BlockState, error) {
	rows, err := d.Query(ctx, `SELECT document_id, block_key, user_id, data, updated_at FROM block_states WHERE document_id = ? AND block_key = ?`, docID, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlockState{}
	for rows.Next() {
		var b BlockState
		if err := rows.Scan(&b.DocumentID, &b.Key, &b.UserID, &b.Data, &b.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// PutBlockState stores a reader's state; empty data removes it.
func (d *DB) PutBlockState(ctx context.Context, projectID string, b BlockState) error {
	if b.Data == "" {
		_, err := d.Exec(ctx, `DELETE FROM block_states WHERE document_id = ? AND block_key = ? AND user_id = ?`, b.DocumentID, b.Key, b.UserID)
		return err
	}
	_, err := d.Exec(ctx, `INSERT INTO block_states (document_id, block_key, user_id, tenant_id, project_id, data, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (document_id, block_key, user_id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
		b.DocumentID, b.Key, b.UserID, TenantOf(ctx), projectID, b.Data, Now())
	return err
}
