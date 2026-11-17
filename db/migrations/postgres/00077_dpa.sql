-- +goose Up
-- Data processing agreement (DPA): versioned texts set by the system admin
-- and acceptances by organization admins (who, when, which text).
CREATE TABLE dpa_versions (
  id           TEXT NOT NULL PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  version      TEXT NOT NULL,
  body_en      TEXT NOT NULL,
  body_de      TEXT NOT NULL DEFAULT '',
  published_at TIMESTAMPTZ NOT NULL,
  created_by   TEXT NOT NULL
);

CREATE TABLE dpa_acceptances (
  id           TEXT NOT NULL PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  org_id       TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  version_id   TEXT NOT NULL,
  version      TEXT NOT NULL,
  lang         TEXT NOT NULL,
  text_sha256  TEXT NOT NULL,
  user_id      TEXT NOT NULL,
  signer_name  TEXT NOT NULL,
  signer_role  TEXT NOT NULL DEFAULT '',
  company      TEXT NOT NULL,
  accepted_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX dpa_acceptances_org ON dpa_acceptances(org_id);

ALTER TABLE dpa_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE dpa_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON dpa_versions USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE dpa_acceptances ENABLE ROW LEVEL SECURITY;
ALTER TABLE dpa_acceptances FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON dpa_acceptances USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE dpa_acceptances;
DROP TABLE dpa_versions;
