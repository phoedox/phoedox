package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// Image kinds.
const (
	ImageUser    = "user"
	ImageProject = "project"
)

// Image is a small picture: a profile picture or a project image.
type Image struct {
	Mime      string
	Data      []byte
	UpdatedAt Time
}

// SetImage stores an image and bumps the owner's version.
func (d *DB) SetImage(ctx context.Context, kind, ownerID, mime string, data []byte) (string, error) {
	now := Now()
	version := strconv.FormatInt(now.UnixMilli(), 36)
	err := d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO images (kind, owner_id, tenant_id, mime, data, updated_at) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (kind, owner_id) DO UPDATE SET mime = excluded.mime, data = excluded.data, updated_at = excluded.updated_at`,
			kind, ownerID, TenantOf(ctx), mime, data, now); err != nil {
			return err
		}
		return setImageVersion(ctx, tx, kind, ownerID, version)
	})
	return version, err
}

func setImageVersion(ctx context.Context, tx *Tx, kind, ownerID, version string) error {
	q := `UPDATE users SET avatar = ? WHERE id = ?`
	if kind == ImageProject {
		q = `UPDATE projects SET image = ? WHERE id = ?`
	}
	_, err := tx.Exec(ctx, q, version, ownerID)
	return err
}

// DeleteImage removes an image.
func (d *DB) DeleteImage(ctx context.Context, kind, ownerID string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM images WHERE kind = ? AND owner_id = ?`, kind, ownerID); err != nil {
			return err
		}
		return setImageVersion(ctx, tx, kind, ownerID, "")
	})
}

// GetImage returns an image.
func (d *DB) GetImage(ctx context.Context, kind, ownerID string) (Image, error) {
	var img Image
	err := d.QueryRow(ctx, `SELECT mime, data, updated_at FROM images WHERE kind = ? AND owner_id = ?`, kind, ownerID).Scan(&img.Mime, &img.Data, &img.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return img, ErrNotFound
	}
	return img, err
}
