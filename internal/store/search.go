package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Snippet markers around matches (private-use characters, safe in JSON;
// clients replace them with highlighting).
const (
	MarkStart = ""
	MarkEnd   = ""
)

// IndexEntry is the searchable text of a document or entity.
type IndexEntry struct {
	RefID     string
	Kind      string // document | entity
	ProjectID string
	Slug      string
	Subtype   string // document type or entity kind
	Title     string
	Body      string
}

// IndexPut adds or replaces an entry.
func (d *DB) IndexPut(ctx context.Context, e IndexEntry) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM search_index WHERE ref_id = ?`, e.RefID); err != nil {
			return err
		}
		// The tenant comes from the project: reindexing runs as system for all tenants.
		_, err := tx.Exec(ctx, `INSERT INTO search_index (ref_id, kind, tenant_id, project_id, slug, subtype, title, body)
			SELECT ?, ?, tenant_id, id, ?, ?, ?, ? FROM projects WHERE id = ?`,
			e.RefID, e.Kind, e.Slug, e.Subtype, e.Title, e.Body, e.ProjectID)
		return err
	})
}

// TranslationRef is the index key of a document's translation.
func TranslationRef(documentID, locale string) string { return documentID + "~" + locale }

// IndexDelete removes an entry (deleted or trashed objects), with the
// translations of a document.
func (d *DB) IndexDelete(ctx context.Context, refID string) error {
	_, err := d.Exec(ctx, `DELETE FROM search_index WHERE ref_id = ? OR ref_id LIKE ? ESCAPE '\'`, refID, escapeLike(refID)+"~%")
	return err
}

// IndexBody returns the indexed text of an entry ("" if none).
func (d *DB) IndexBody(ctx context.Context, refID string) (string, error) {
	var body string
	err := d.QueryRow(ctx, `SELECT body FROM search_index WHERE ref_id = ?`, refID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return body, err
}

// IndexCount returns the number of indexed entries.
func (d *DB) IndexCount(ctx context.Context) (int, error) {
	var n int
	err := d.QueryRow(ctx, `SELECT count(*) FROM search_index`).Scan(&n)
	return n, err
}

// IndexIncomplete tells whether translations or tasks exist but none of
// them is in the index (instances updated from before they were searchable).
func (d *DB) IndexIncomplete(ctx context.Context) (bool, error) {
	for _, q := range [][2]string{
		{`SELECT count(*) FROM document_translations`, `SELECT count(*) FROM search_index WHERE kind = 'document' AND ref_id LIKE '%~%'`},
		{`SELECT count(*) FROM tasks`, `SELECT count(*) FROM search_index WHERE kind = 'task'`},
	} {
		var have, indexed int
		if err := d.QueryRow(ctx, q[0]).Scan(&have); err != nil {
			return false, err
		}
		if have == 0 {
			continue
		}
		if err := d.QueryRow(ctx, q[1]).Scan(&indexed); err != nil {
			return false, err
		}
		if indexed == 0 {
			return true, nil
		}
	}
	return false, nil
}

// Hit is a search result.
type Hit struct {
	Type        string  `json:"type"` // document | entity
	ProjectID   string  `json:"projectId"`
	ProjectSlug string  `json:"projectSlug"`
	ProjectName string  `json:"projectName"`
	ID          string  `json:"id"`
	Slug        string  `json:"slug"`
	Title       string  `json:"title"`
	Kind        string  `json:"kind"`             // document type or entity kind
	Locale      string  `json:"locale,omitempty"` // set when a translation matched
	Snippet     string  `json:"snippet,omitempty"`
	Score       float64 `json:"-"`
}

var wordRe = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}._-]*`)

// queryWords splits a query into lower-case words (max 8).
func queryWords(q string) []string {
	words := wordRe.FindAllString(strings.ToLower(q), -1)
	if len(words) > 8 {
		words = words[:8]
	}
	return words
}

// Search finds documents and entities in the given projects. All words
// must occur (as word, prefix or part of a word); results are ranked with
// title matches first.
func (d *DB) Search(ctx context.Context, projectIDs []string, query string, limit int) ([]Hit, error) {
	words := queryWords(query)
	if len(words) == 0 || len(projectIDs) == 0 {
		return []Hit{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	var (
		hits []Hit
		err  error
	)
	if d.Dialect == Postgres {
		hits, err = d.searchPostgres(ctx, projectIDs, query, words, limit)
	} else {
		hits, err = d.searchSQLite(ctx, projectIDs, words, limit)
	}
	if err != nil {
		return nil, err
	}
	// A document and its translations count once: the best match wins.
	seen := map[string]bool{}
	out := hits[:0]
	for _, h := range hits {
		if key := h.Type + "/" + h.ID; !seen[key] {
			seen[key] = true
			out = append(out, h)
		}
	}
	return out, nil
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?, ", n), ", ") }

const bodyPrefix = 40000 // characters read for snippets

func (d *DB) searchPostgres(ctx context.Context, projectIDs []string, query string, words []string, limit int) ([]Hit, error) {
	// to_tsquery input: sanitized words as prefixes.
	terms := make([]string, len(words))
	var conds []string
	args := []any{}
	for i, w := range words {
		terms[i] = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return r
			}
			return ' '
		}, w)
		terms[i] = strings.Join(strings.Fields(terms[i]), ":* & ") + ":*"
	}
	tsq := strings.Join(terms, " & ")
	for _, w := range words {
		pat := "%" + escapeLike(w) + "%"
		conds = append(conds, `(lower(s.title) LIKE ? ESCAPE '\' OR lower(s.body) LIKE ? ESCAPE '\')`)
		args = append(args, pat, pat)
	}
	q := strings.ToLower(strings.Join(words, " "))
	all := []any{tsq, q, TenantOf(ctx)}
	for _, p := range projectIDs {
		all = append(all, p)
	}
	all = append(all, args...)
	rows, err := d.Query(ctx, `WITH q AS (SELECT to_tsquery('simple', ?) AS tsq, CAST(? AS text) AS raw)
		SELECT s.kind, s.project_id, p.slug, p.name, s.ref_id, s.slug, s.title, s.subtype, substr(s.body, 1, `+strconv.Itoa(bodyPrefix)+`),
		  ts_rank_cd(s.vec, q.tsq) + 2 * public.similarity(lower(s.title), q.raw) + CASE WHEN lower(s.title) LIKE '%' || q.raw || '%' THEN 1 ELSE 0 END AS score
		FROM search_index s CROSS JOIN q JOIN projects p ON p.id = s.project_id
		WHERE s.tenant_id = ? AND s.project_id IN (`+placeholders(len(projectIDs))+`)
		  AND (s.vec @@ q.tsq OR (`+strings.Join(conds, " AND ")+`) OR public.word_similarity(q.raw, lower(s.title)) > 0.5)
		ORDER BY score DESC, s.title LIMIT `+strconv.Itoa(limit), all...)
	if err != nil {
		return nil, err
	}
	return scanHits(rows, words)
}

func (d *DB) searchSQLite(ctx context.Context, projectIDs []string, words []string, limit int) ([]Hit, error) {
	var phrases, likes []string
	var likeArgs []any
	for _, w := range words {
		if utf8.RuneCountInString(w) >= 3 {
			phrases = append(phrases, `"`+strings.ReplaceAll(w, `"`, `""`)+`"`)
		} else {
			// Trigrams need three characters; short words use LIKE.
			likes = append(likes, `(title LIKE ? ESCAPE '\' OR body LIKE ? ESCAPE '\')`)
			pat := "%" + escapeLike(w) + "%"
			likeArgs = append(likeArgs, pat, pat)
		}
	}
	where := []string{`tenant_id = ?`, `project_id IN (` + placeholders(len(projectIDs)) + `)`}
	args := []any{TenantOf(ctx)}
	for _, p := range projectIDs {
		args = append(args, p)
	}
	rank := `0`
	if len(phrases) > 0 {
		where = append(where, `search_index MATCH ?`)
		args = append(args, strings.Join(phrases, " AND "))
		// Columns: ref_id, kind, tenant_id, project_id, slug, subtype, title, body.
		rank = `-bm25(search_index, 0, 0, 0, 0, 0, 0, 10.0, 1.0)`
	}
	where = append(where, likes...)
	args = append(args, likeArgs...)
	rows, err := d.Query(ctx, `SELECT s.kind, s.project_id, p.slug, p.name, s.ref_id, s.slug, s.title, s.subtype, s.body, s.score FROM (
		  SELECT kind, project_id, ref_id, slug, title, subtype, substr(body, 1, `+strconv.Itoa(bodyPrefix)+`) AS body, `+rank+` AS score
		  FROM search_index WHERE `+strings.Join(where, " AND ")+` ORDER BY score DESC LIMIT `+strconv.Itoa(limit*2)+`
		) s JOIN projects p ON p.id = s.project_id`, args...)
	if err != nil {
		return nil, err
	}
	hits, err := scanHits(rows, words)
	if err != nil {
		return nil, err
	}
	// Title matches first (bm25 alone does not prefer short titles enough).
	q := strings.Join(words, " ")
	for i := range hits {
		if strings.Contains(strings.ToLower(hits[i].Title), q) {
			hits[i].Score += 100
		}
	}
	sortHits(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func scanHits(rows interface {
	Next() bool
	Scan(...any) error
	Close() error
	Err() error
}, words []string) ([]Hit, error) {
	defer rows.Close()
	hits := []Hit{}
	for rows.Next() {
		var h Hit
		var body string
		if err := rows.Scan(&h.Type, &h.ProjectID, &h.ProjectSlug, &h.ProjectName, &h.ID, &h.Slug, &h.Title, &h.Kind, &body, &h.Score); err != nil {
			return nil, err
		}
		if id, loc, ok := strings.Cut(h.ID, "~"); ok && h.Type == "document" {
			h.ID, h.Locale = id, loc
		}
		h.Snippet = Snippet(body, words, 180)
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func sortHits(h []Hit) {
	for i := 1; i < len(h); i++ {
		for j := i; j > 0 && (h[j].Score > h[j-1].Score); j-- {
			h[j], h[j-1] = h[j-1], h[j]
		}
	}
}

// fold lowercases and removes common diacritics for matching.
func fold(r rune) rune {
	r = unicode.ToLower(r)
	switch r {
	case 'ä', 'á', 'à', 'â':
		return 'a'
	case 'ö', 'ó', 'ò', 'ô':
		return 'o'
	case 'ü', 'ú', 'ù', 'û':
		return 'u'
	case 'é', 'è', 'ê', 'ë':
		return 'e'
	}
	return r
}

// Snippet returns about width characters of text around the first match,
// with all matches wrapped in MarkStart/MarkEnd. Empty if nothing matches.
func Snippet(text string, words []string, width int) string {
	runes := []rune(text)
	folded := make([]rune, len(runes))
	for i, r := range runes {
		folded[i] = fold(r)
	}
	type span struct{ from, to int }
	var spans []span
	for _, w := range words {
		wr := []rune(w)
		for i := range wr {
			wr[i] = fold(wr[i])
		}
		if len(wr) == 0 {
			continue
		}
		for i := 0; i+len(wr) <= len(folded); i++ {
			if string(folded[i:i+len(wr)]) == string(wr) {
				spans = append(spans, span{i, i + len(wr)})
				i += len(wr) - 1
			}
		}
	}
	if len(spans) == 0 {
		return ""
	}
	first := spans[0]
	for _, s := range spans {
		if s.from < first.from {
			first = s
		}
	}
	start := max(0, first.from-width/3)
	end := min(len(runes), start+width)
	// Move start to a word boundary.
	for start > 0 && start < first.from && !unicode.IsSpace(runes[start-1]) {
		start++
	}
	var b strings.Builder
	if start > 0 {
		b.WriteString("…")
	}
	for i := start; i < end; i++ {
		for _, s := range spans {
			if s.from == i && s.to <= end {
				b.WriteString(MarkStart)
			}
		}
		r := runes[i]
		if r == '\n' || r == '\t' {
			r = ' '
		}
		b.WriteRune(r)
		for _, s := range spans {
			if s.to == i+1 && s.from >= start {
				b.WriteString(MarkEnd)
			}
		}
	}
	if end < len(runes) {
		b.WriteString("…")
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// AllDocuments returns all live documents with content (for reindexing).
func (d *DB) AllDocuments(ctx context.Context) ([]Document, error) {
	rows, err := d.Query(ctx, `SELECT `+docCols+`, content FROM documents WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		doc, err := scanDoc(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

// AllEntities returns all live entities (for reindexing).
func (d *DB) AllEntities(ctx context.Context) ([]Entity, error) {
	rows, err := d.Query(ctx, `SELECT `+entityCols+` FROM entities WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entity
	for rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
