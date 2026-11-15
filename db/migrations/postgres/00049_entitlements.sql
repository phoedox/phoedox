-- +goose Up
-- Entitlements (ADR-0061): which organization or user may use a paid
-- feature, and why. source is grandfathered (signed up before the feature
-- became paid; never removed), plan (billing) or grant (system admin).
-- subject is "org:<id>" or "user:<id>".
CREATE TABLE entitlements (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  subject    TEXT NOT NULL,
  feature    TEXT NOT NULL,
  source     TEXT NOT NULL,
  granted_at TIMESTAMPTZ NOT NULL,
  granted_by TEXT NOT NULL DEFAULT '',
  note       TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (subject, feature)
);
ALTER TABLE entitlements ENABLE ROW LEVEL SECURITY;
ALTER TABLE entitlements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON entitlements USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE entitlements;
