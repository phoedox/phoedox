package store

import (
	"context"
	"strconv"
	"strings"
)

// Hit is a search result.
type Hit struct {
	Type      string `json:"type"` // document | entity
	ProjectID string `json:"projectId"`
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Kind      string `json:"kind"` // document type or entity kind
	Category  string `json:"category,omitempty"`
}

// Search finds documents and entities in the given projects whose title,
// slug or content contains all words of the query.
// MVP: substring match; full text with trigram tolerance follows.
func (d *DB) Search(ctx context.Context, projectIDs []string, query string, limit int) ([]Hit, error) {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 || len(projectIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	in := strings.TrimSuffix(strings.Repeat("?, ", len(projectIDs)), ", ")
	like := d.Dialect.like()

	docWhere, entWhere := []string{}, []string{}
	var docArgs, entArgs []any
	for _, p := range projectIDs {
		docArgs = append(docArgs, p)
		entArgs = append(entArgs, p)
	}
	for _, w := range words {
		pat := "%" + escapeLike(w) + "%"
		docWhere = append(docWhere, `(title `+like+` ? ESCAPE '\' OR slug `+like+` ? ESCAPE '\' OR `+d.Dialect.textOf("content")+` `+like+` ? ESCAPE '\')`)
		docArgs = append(docArgs, pat, pat, pat)
		entWhere = append(entWhere, `(name `+like+` ? ESCAPE '\' OR slug `+like+` ? ESCAPE '\' OR kind `+like+` ? ESCAPE '\' OR `+d.Dialect.textOf("attrs")+` `+like+` ? ESCAPE '\')`)
		entArgs = append(entArgs, pat, pat, pat, pat)
	}
	var hits []Hit
	rows, err := d.Query(ctx, `SELECT project_id, id, slug, title, doc_type, category FROM documents
		WHERE deleted_at IS NULL AND project_id IN (`+in+`) AND `+strings.Join(docWhere, " AND ")+` ORDER BY updated_at DESC LIMIT `+strconv.Itoa(limit), docArgs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		h := Hit{Type: "document"}
		if err := rows.Scan(&h.ProjectID, &h.ID, &h.Slug, &h.Title, &h.Kind, &h.Category); err != nil {
			rows.Close()
			return nil, err
		}
		hits = append(hits, h)
	}
	rows.Close()
	rows, err = d.Query(ctx, `SELECT project_id, id, slug, name, kind FROM entities
		WHERE deleted_at IS NULL AND project_id IN (`+in+`) AND `+strings.Join(entWhere, " AND ")+` ORDER BY name LIMIT `+strconv.Itoa(limit), entArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		h := Hit{Type: "entity"}
		if err := rows.Scan(&h.ProjectID, &h.ID, &h.Slug, &h.Title, &h.Kind); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
