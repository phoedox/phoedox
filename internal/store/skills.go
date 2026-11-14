package store

import (
	"context"
	"database/sql"
	"errors"
)

// Skill marks a page as an AI skill: a named set of instructions that AI
// clients load when a task matches the description.
type Skill struct {
	DocumentID  string `json:"documentId"`
	ProjectID   string `json:"-"`
	Name        string `json:"name"`
	Description string `json:"description"`
	UpdatedBy   string `json:"-"`
	UpdatedAt   Time   `json:"updatedAt"`
}

const skillCols = `document_id, project_id, name, description, updated_by, updated_at`

func scanSkill(r interface{ Scan(...any) error }) (Skill, error) {
	var x Skill
	err := r.Scan(&x.DocumentID, &x.ProjectID, &x.Name, &x.Description, &x.UpdatedBy, &x.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

// GetSkill returns the skill of a page (ErrNotFound if it is none).
func (d *DB) GetSkill(ctx context.Context, documentID string) (Skill, error) {
	return scanSkill(d.QueryRow(ctx, `SELECT `+skillCols+` FROM doc_skills WHERE document_id = ?`, documentID))
}

// SkillByName finds a live skill of a project by name.
func (d *DB) SkillByName(ctx context.Context, projectID, name string) (Skill, error) {
	return scanSkill(d.QueryRow(ctx, `SELECT `+prefixed("s.", skillCols)+` FROM doc_skills s JOIN documents doc ON doc.id = s.document_id
		WHERE s.project_id = ? AND s.name = ? AND doc.deleted_at IS NULL`, projectID, name))
}

// SaveSkill marks a page as a skill or changes its name and description.
// A name used by another page of the project returns ErrConflict.
func (d *DB) SaveSkill(ctx context.Context, x Skill) error {
	_, err := d.Exec(ctx, `INSERT INTO doc_skills (`+skillCols+`, tenant_id) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (document_id) DO UPDATE SET name = excluded.name, description = excluded.description, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		x.DocumentID, x.ProjectID, x.Name, x.Description, x.UpdatedBy, x.UpdatedAt, TenantOf(ctx))
	if err != nil && isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// DeleteSkill turns a page back into an ordinary page.
func (d *DB) DeleteSkill(ctx context.Context, documentID string) error {
	_, err := d.Exec(ctx, `DELETE FROM doc_skills WHERE document_id = ?`, documentID)
	return err
}

// Skills lists the skills of a project's live pages by name.
func (d *DB) Skills(ctx context.Context, projectID string) ([]Skill, error) {
	rows, err := d.Query(ctx, `SELECT `+prefixed("s.", skillCols)+` FROM doc_skills s JOIN documents doc ON doc.id = s.document_id
		WHERE s.project_id = ? AND doc.deleted_at IS NULL ORDER BY s.name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Skill{}
	for rows.Next() {
		x, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
