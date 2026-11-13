-- +goose Up
-- Full-text search: FTS5 with the trigram tokenizer finds parts of words
-- (e.g. "router" in "Hauptrouter"), case and accent insensitive.
CREATE VIRTUAL TABLE search_index USING fts5(
  ref_id UNINDEXED, kind UNINDEXED, tenant_id UNINDEXED, project_id UNINDEXED, slug UNINDEXED, subtype UNINDEXED,
  title, body,
  tokenize = 'trigram remove_diacritics 1'
);

-- +goose Down
DROP TABLE search_index;
