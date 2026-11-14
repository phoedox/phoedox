-- +goose Up
-- An organization's own address for the app (e.g. docs.example.com). It is
-- used only after a DNS TXT record proved control over the domain.
CREATE TABLE org_domains (
  org_id      TEXT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  domain      TEXT NOT NULL UNIQUE,
  token       TEXT NOT NULL,
  verified_at TIMESTAMPTZ,
  created_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ NOT NULL
);
ALTER TABLE org_domains ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_domains FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON org_domains USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE org_domains;
