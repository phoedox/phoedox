-- +goose Up
ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,                    -- sha256 of the cookie value
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE api_tokens (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  token_hash   TEXT NOT NULL UNIQUE,              -- sha256 of the token
  prefix       TEXT NOT NULL,                     -- first characters, shown in the UI
  created_at   TIMESTAMPTZ NOT NULL,
  last_used_at TIMESTAMPTZ,
  revoked_at   TIMESTAMPTZ
);

ALTER TABLE projects ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN ai_write_mode TEXT NOT NULL DEFAULT 'suggest';

ALTER TABLE documents ADD COLUMN slug TEXT NOT NULL DEFAULT '';
ALTER TABLE documents ADD COLUMN category TEXT NOT NULL DEFAULT '';
ALTER TABLE documents ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
ALTER TABLE documents ADD COLUMN created_by TEXT NOT NULL DEFAULT '';
ALTER TABLE documents ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
CREATE UNIQUE INDEX documents_slug ON documents (project_id, slug) WHERE deleted_at IS NULL;

CREATE TABLE document_versions (
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  version     INTEGER NOT NULL,
  title       TEXT NOT NULL,
  content     JSONB NOT NULL,
  actor       TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (document_id, version)
);

-- documents relation: entities mentioned in a document
CREATE TABLE document_links (
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  entity_id   TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
  PRIMARY KEY (document_id, entity_id)
);

-- Pending AI/MCP changes in projects with ai_write_mode = 'suggest'
CREATE TABLE suggestions (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  actor       TEXT NOT NULL,
  op          TEXT NOT NULL,
  summary     TEXT NOT NULL,
  payload     JSONB NOT NULL,
  status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'rejected')),
  created_at  TIMESTAMPTZ NOT NULL,
  decided_by  TEXT,
  decided_at  TIMESTAMPTZ
);
CREATE INDEX suggestions_open ON suggestions (project_id, status);

-- +goose Down
DROP TABLE suggestions;
DROP TABLE document_links;
DROP TABLE document_versions;
DROP INDEX documents_slug;
ALTER TABLE documents DROP COLUMN version;
ALTER TABLE documents DROP COLUMN created_by;
ALTER TABLE documents DROP COLUMN position;
ALTER TABLE documents DROP COLUMN category;
ALTER TABLE documents DROP COLUMN slug;
ALTER TABLE projects DROP COLUMN ai_write_mode;
ALTER TABLE projects DROP COLUMN description;
DROP TABLE api_tokens;
DROP TABLE sessions;
ALTER TABLE users DROP COLUMN is_admin;
ALTER TABLE users DROP COLUMN password_hash;
