-- +goose Up
-- Agents, stage 1 (ADR-0033): read-only reports over HTTPS with an agent token.
CREATE TABLE agent_enrollments (
  code_hash  TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  uses_left  INTEGER NOT NULL
);
CREATE TABLE agents (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, -- acts with this owner's rights
  token_hash  TEXT NOT NULL UNIQUE,
  hostname    TEXT NOT NULL,
  entity_slug TEXT NOT NULL DEFAULT '',
  version     TEXT NOT NULL DEFAULT '',
  last_report TEXT,
  last_seen   TEXT,
  revoked_at  TEXT,
  created_at  TEXT NOT NULL
);
CREATE INDEX agents_project ON agents (project_id);

-- +goose Down
DROP TABLE agents;
DROP TABLE agent_enrollments;
