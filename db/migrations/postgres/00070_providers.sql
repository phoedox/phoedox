-- +goose Up
-- Service providers (ADR-0073): a customer organization grants a provider
-- organization a role in all or chosen projects, optionally until a date.
-- Members of the provider then work in those projects; their actions are
-- logged "via provider".
CREATE TABLE provider_links (
  id              TEXT PRIMARY KEY,
  tenant_id       TEXT NOT NULL REFERENCES tenants(id),
  customer_org_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  provider_org_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  role            TEXT NOT NULL,
  projects        TEXT NOT NULL DEFAULT '',
  expires_at      TIMESTAMPTZ,
  note            TEXT NOT NULL DEFAULT '',
  created_by      TEXT NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL,
  revoked_at      TIMESTAMPTZ,
  UNIQUE (customer_org_id, provider_org_id)
);
CREATE INDEX provider_links_provider ON provider_links(provider_org_id);
ALTER TABLE provider_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE provider_links FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON provider_links USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE provider_links;
