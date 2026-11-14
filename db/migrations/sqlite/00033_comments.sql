-- +goose Up
-- Comment threads on pages; anchor quotes the block a thread belongs to
-- (empty = the whole page). Replies point to the first comment of a thread.
CREATE TABLE comments (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  parent_id   TEXT REFERENCES comments(id) ON DELETE CASCADE,
  anchor      TEXT NOT NULL DEFAULT '',
  body        TEXT NOT NULL,
  mentions    TEXT NOT NULL DEFAULT '',
  author_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  actor       TEXT NOT NULL,
  resolved_by TEXT REFERENCES users(id) ON DELETE SET NULL,
  resolved_at TEXT,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  deleted_at  TEXT
);
CREATE INDEX comments_document ON comments(document_id);

-- +goose Down
DROP TABLE comments;
