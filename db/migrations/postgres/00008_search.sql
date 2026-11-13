-- +goose Up
-- Full-text search: one index table maintained by the app (title + plain
-- text). tsvector for words and prefixes, trigrams for parts of words and
-- typos. pg_trgm is a trusted extension (database owners may create it);
-- it lives in public, so references are schema-qualified.
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

CREATE TABLE search_index (
  ref_id     TEXT PRIMARY KEY,                -- document or entity id
  kind       TEXT NOT NULL,                   -- document | entity
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  slug       TEXT NOT NULL,
  subtype    TEXT NOT NULL,                   -- document type or entity kind
  title      TEXT NOT NULL,
  body       TEXT NOT NULL DEFAULT '',
  vec        tsvector GENERATED ALWAYS AS (
    setweight(to_tsvector('simple', title), 'A') || setweight(to_tsvector('simple', body), 'B')
  ) STORED
);
CREATE INDEX search_index_vec ON search_index USING GIN (vec);
CREATE INDEX search_index_title_trgm ON search_index USING GIN (lower(title) public.gin_trgm_ops);
CREATE INDEX search_index_body_trgm ON search_index USING GIN (lower(body) public.gin_trgm_ops);
CREATE INDEX search_index_project ON search_index (project_id);

-- +goose Down
DROP TABLE search_index;
