-- +goose Up
-- Responsible person and review interval per entry.
CREATE TABLE entity_meta (
  entity_id   TEXT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  owner_id    TEXT REFERENCES users(id) ON DELETE SET NULL,
  review_days INTEGER NOT NULL DEFAULT 0,
  reviewed_at TIMESTAMPTZ,
  reviewed_by TEXT NOT NULL DEFAULT '',
  notified_at TIMESTAMPTZ,
  updated_by  TEXT NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX entity_meta_project ON entity_meta(project_id);
CREATE INDEX entity_meta_owner ON entity_meta(owner_id);
ALTER TABLE entity_meta ENABLE ROW LEVEL SECURITY;
ALTER TABLE entity_meta FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON entity_meta USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- Documentation rules per kind (location, page, owner, required fields, review).
CREATE TABLE kind_rules (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  rules      TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (project_id, kind)
);
ALTER TABLE kind_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE kind_rules FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON kind_rules USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE kind_rules;
DROP TABLE entity_meta;
