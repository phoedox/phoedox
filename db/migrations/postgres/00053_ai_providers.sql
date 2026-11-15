-- +goose Up
-- Model providers of the AI chat (ADR-0067): set up by system admins for
-- the instance (org_id '') or by organization admins for their organization
-- (own key). Models with their prices are a JSON list; the API key is sealed
-- with the master key. Chats and messages remember the model, the usage
-- ledger the provider and whether the organization's own key paid.
CREATE TABLE ai_providers (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  org_id      TEXT NOT NULL DEFAULT '',
  name        TEXT NOT NULL,
  kind        TEXT NOT NULL,
  base_url    TEXT NOT NULL DEFAULT '',
  api_key     TEXT NOT NULL DEFAULT '',
  effort      TEXT NOT NULL DEFAULT '',
  models      TEXT NOT NULL DEFAULT '[]',
  enabled     BOOLEAN NOT NULL DEFAULT TRUE,
  created_by  TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX ai_providers_org ON ai_providers(org_id);
ALTER TABLE ai_providers ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_providers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ai_providers USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE ai_chats ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_chat_messages ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_usage ADD COLUMN provider_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_usage ADD COLUMN own_key BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE organizations ADD COLUMN ai_chat_model TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE organizations DROP COLUMN ai_chat_model;
ALTER TABLE ai_usage DROP COLUMN own_key;
ALTER TABLE ai_usage DROP COLUMN provider_id;
ALTER TABLE ai_chat_messages DROP COLUMN model;
ALTER TABLE ai_chats DROP COLUMN model;
DROP TABLE ai_providers;
