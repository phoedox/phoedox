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
  resolved_at TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL,
  deleted_at  TIMESTAMPTZ
);
CREATE INDEX comments_document ON comments(document_id);
ALTER TABLE comments ENABLE ROW LEVEL SECURITY;
ALTER TABLE comments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON comments USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE comments;
