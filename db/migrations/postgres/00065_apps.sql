-- +goose Up
-- Apps (apps-definition): what a person allowed an app to do (bridge level),
-- and the app's own storage (shared or per person), counted against the
-- project storage with a limit per app.
CREATE TABLE app_grants (
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  level       INTEGER NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (document_id, user_id)
);

CREATE TABLE app_storage (
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  user_id     TEXT NOT NULL DEFAULT '',
  item_key    TEXT NOT NULL,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  value       TEXT NOT NULL,
  size        INTEGER NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (document_id, user_id, item_key)
);
CREATE INDEX app_storage_project ON app_storage(project_id);
ALTER TABLE app_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON app_grants USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE app_storage ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_storage FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON app_storage USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE app_storage;
DROP TABLE app_grants;
