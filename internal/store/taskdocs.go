package store

import "context"

// TaskDoc is a document linked to a task card (ADR-0064).
type TaskDoc struct {
	TaskID string `json:"taskId,omitempty"`
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Number int    `json:"number,omitempty"` // decisions
	// Restricted is used by the core to hide pages the reader may not see.
	Restricted bool `json:"-"`
}

// TaskDocs lists the documents linked to the cards of a project (or of
// one task), leaving out documents in the trash.
func (d *DB) TaskDocs(ctx context.Context, projectID, taskID string) ([]TaskDoc, error) {
	q := `SELECT td.task_id, doc.id, doc.slug, doc.title, doc.doc_type, COALESCE(doc.number, 0), doc.restricted
		FROM task_documents td JOIN documents doc ON doc.id = td.document_id
		WHERE td.project_id = ? AND doc.deleted_at IS NULL`
	args := []any{projectID}
	if taskID != "" {
		q += ` AND td.task_id = ?`
		args = append(args, taskID)
	}
	rows, err := d.Query(ctx, q+` ORDER BY td.position, td.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskDoc{}
	for rows.Next() {
		var x TaskDoc
		if err := rows.Scan(&x.TaskID, &x.ID, &x.Slug, &x.Title, &x.Type, &x.Number, &x.Restricted); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SetTaskDocs replaces the documents of a task (in the given order).
func (d *DB) SetTaskDocs(ctx context.Context, projectID, taskID string, docIDs []string, actor string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM task_documents WHERE task_id = ? AND project_id = ?`, taskID, projectID); err != nil {
			return err
		}
		for i, id := range docIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO task_documents (task_id, document_id, tenant_id, project_id, position, created_by, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, taskID, id, TenantOf(ctx), projectID, i, actor, Now()); err != nil {
				return err
			}
		}
		return nil
	})
}

// TasksOfDocument lists the cards a document is linked to.
func (d *DB) TasksOfDocument(ctx context.Context, projectID, docID string) ([]Task, error) {
	rows, err := d.Query(ctx, `SELECT `+taskCols+taskFrom+` JOIN task_documents td ON td.task_id = t.id
		WHERE td.document_id = ? AND t.project_id = ? AND d.deleted_at IS NULL ORDER BY t.done_at IS NOT NULL, t.updated_at DESC`, docID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
