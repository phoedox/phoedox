package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Comment is a comment on a page. The first comment of a thread has no
// parent; replies point to it. Anchor quotes the block the thread is about.
type Comment struct {
	ID         string   `json:"id"`
	ProjectID  string   `json:"projectId"`
	DocumentID string   `json:"documentId"`
	ParentID   string   `json:"parentId,omitempty"`
	Anchor     string   `json:"anchor,omitempty"`
	Body       string   `json:"body"`
	Mentions   []string `json:"mentions"`
	AuthorID   string   `json:"authorId"`
	AuthorName string   `json:"authorName,omitempty"`
	AuthorPic  string   `json:"authorAvatar,omitempty"`
	Actor      string   `json:"actor"`
	ResolvedBy string   `json:"resolvedBy,omitempty"`
	ResolvedAt Time     `json:"resolvedAt"`
	CreatedAt  Time     `json:"createdAt"`
	UpdatedAt  Time     `json:"updatedAt"`
}

const commentCols = `c.id, c.project_id, c.document_id, c.parent_id, c.anchor, c.body, c.mentions, c.author_id, COALESCE(u.name, ''), COALESCE(u.avatar, ''), c.actor, c.resolved_by, c.resolved_at, c.created_at, c.updated_at`

const commentFrom = ` FROM comments c LEFT JOIN users u ON u.id = c.author_id`

func scanComment(r interface{ Scan(...any) error }) (Comment, error) {
	var c Comment
	var parent, resolvedBy sql.NullString
	var mentions string
	err := r.Scan(&c.ID, &c.ProjectID, &c.DocumentID, &parent, &c.Anchor, &c.Body, &mentions, &c.AuthorID, &c.AuthorName, &c.AuthorPic, &c.Actor,
		&resolvedBy, &c.ResolvedAt, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	c.ParentID, c.ResolvedBy = parent.String, resolvedBy.String
	c.Mentions = []string{}
	if mentions != "" {
		c.Mentions = strings.Split(mentions, ",")
	}
	return c, err
}

// AddComment stores a comment.
func (d *DB) AddComment(ctx context.Context, c Comment) (Comment, error) {
	c.CreatedAt, c.UpdatedAt = Now(), Now()
	_, err := d.Exec(ctx, `INSERT INTO comments (id, tenant_id, project_id, document_id, parent_id, anchor, body, mentions, author_id, actor, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, TenantOf(ctx), c.ProjectID, c.DocumentID, nullable(c.ParentID), c.Anchor, c.Body, strings.Join(c.Mentions, ","), c.AuthorID, c.Actor, c.CreatedAt, c.UpdatedAt)
	return c, err
}

// GetComment returns a comment of a project that is not deleted.
func (d *DB) GetComment(ctx context.Context, projectID, id string) (Comment, error) {
	return scanComment(d.QueryRow(ctx, `SELECT `+commentCols+commentFrom+` WHERE c.project_id = ? AND c.id = ? AND c.deleted_at IS NULL`, projectID, id))
}

// DocumentComments lists the comments of a page, oldest first.
func (d *DB) DocumentComments(ctx context.Context, documentID string) ([]Comment, error) {
	rows, err := d.Query(ctx, `SELECT `+commentCols+commentFrom+` WHERE c.document_id = ? AND c.deleted_at IS NULL ORDER BY c.created_at, c.id`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// EditComment replaces the text of a comment.
func (d *DB) EditComment(ctx context.Context, id, body string, mentions []string) error {
	res, err := d.Exec(ctx, `UPDATE comments SET body = ?, mentions = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, body, strings.Join(mentions, ","), Now(), id)
	return affected(res, err)
}

// ResolveComment marks a thread as done (by = "" reopens it).
func (d *DB) ResolveComment(ctx context.Context, id, by string) error {
	var at any
	if by != "" {
		at = Now()
	}
	res, err := d.Exec(ctx, `UPDATE comments SET resolved_by = ?, resolved_at = ? WHERE id = ? AND parent_id IS NULL AND deleted_at IS NULL`, nullable(by), at, id)
	return affected(res, err)
}

// DeleteComment removes a comment; deleting the first comment removes the thread.
func (d *DB) DeleteComment(ctx context.Context, id string) error {
	now := Now()
	res, err := d.Exec(ctx, `UPDATE comments SET deleted_at = ? WHERE (id = ? OR parent_id = ?) AND deleted_at IS NULL`, now, id, id)
	return affected(res, err)
}

// OpenCommentCounts counts the open threads per page of a project.
func (d *DB) OpenCommentCounts(ctx context.Context, projectID string) (map[string]int, error) {
	rows, err := d.Query(ctx, `SELECT document_id, COUNT(*) FROM comments WHERE project_id = ? AND parent_id IS NULL AND resolved_at IS NULL AND deleted_at IS NULL GROUP BY document_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
