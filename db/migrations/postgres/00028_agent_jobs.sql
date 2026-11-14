-- +goose Up
-- Agent actions (ADR-0036): people sign jobs in the browser with a key that
-- never leaves their device; agents check the signature against agent.yaml.
CREATE TABLE signing_keys (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  public_key TEXT NOT NULL, -- base64 ed25519
  created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX signing_keys_user ON signing_keys (user_id);
CREATE TABLE agent_jobs (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  entity_slug TEXT NOT NULL DEFAULT '',
  action      TEXT NOT NULL,
  payload     TEXT NOT NULL,          -- the exact JSON that is signed
  signature   TEXT NOT NULL DEFAULT '',
  key_id      TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL,          -- unsigned, approved, sent, done, failed
  ok          BOOLEAN NOT NULL DEFAULT FALSE,
  output      TEXT NOT NULL DEFAULT '',
  created_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ NOT NULL,
  expires_at  TIMESTAMPTZ NOT NULL,
  finished_at TIMESTAMPTZ
);
CREATE INDEX agent_jobs_agent ON agent_jobs (agent_id, status);
CREATE INDEX agent_jobs_entity ON agent_jobs (project_id, entity_slug, created_at);
ALTER TABLE signing_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE signing_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON signing_keys USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE agent_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_jobs USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE agent_jobs;
DROP TABLE signing_keys;
