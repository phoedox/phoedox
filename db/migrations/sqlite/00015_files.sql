-- +goose Up
-- Uploaded files. Contents are stored once per SHA-256 hash in the blob
-- store; rows hold names and where a file is attached (document or entry).
CREATE TABLE files (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  sha256      TEXT NOT NULL,
  name        TEXT NOT NULL,
  mime        TEXT NOT NULL,
  size        INTEGER NOT NULL,
  document_id TEXT,
  entity_id   TEXT,
  created_by  TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  deleted_at  TEXT
);
CREATE INDEX files_project ON files (project_id, created_at);
CREATE INDEX files_document ON files (document_id);
CREATE INDEX files_entity ON files (entity_id);
CREATE INDEX files_sha ON files (sha256);

-- +goose Down
DROP TABLE files;
