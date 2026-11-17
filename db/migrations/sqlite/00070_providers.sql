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
  expires_at      TEXT,
  note            TEXT NOT NULL DEFAULT '',
  created_by      TEXT NOT NULL,
  created_at      TEXT NOT NULL,
  revoked_at      TEXT,
  UNIQUE (customer_org_id, provider_org_id)
);
CREATE INDEX provider_links_provider ON provider_links(provider_org_id);

-- +goose Down
DROP TABLE provider_links;
