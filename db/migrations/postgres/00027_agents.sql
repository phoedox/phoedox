-- +goose Up
-- Agents, stage 1 (ADR-0033): read-only reports over HTTPS with an agent token.
CREATE TABLE agent_enrollments (
  code_hash  TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TIMESTAMPTZ NOT NULL,
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
  last_report JSONB,
  last_seen   TIMESTAMPTZ,
  revoked_at  TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX agents_project ON agents (project_id);
ALTER TABLE agent_enrollments ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_enrollments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_enrollments USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE agents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agents USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE agents;
DROP TABLE agent_enrollments;
