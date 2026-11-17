-- +goose Up
-- Apps like artifacts: records of collections (a small database per app,
-- shared or per person), consent per device feature and person, and what
-- a file of an app is for (an upload by people, an asset of the author, a
-- preview picture).
CREATE TABLE app_records (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  collection  TEXT NOT NULL,
  owner       TEXT NOT NULL DEFAULT '',
  data        TEXT NOT NULL,
  size        INTEGER NOT NULL,
  created_by  TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX app_records_collection ON app_records (document_id, collection, owner, created_at);
CREATE INDEX app_records_project ON app_records (project_id);

CREATE TABLE app_device_grants (
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device      TEXT NOT NULL,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  allowed     INTEGER NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (document_id, user_id, device)
);

ALTER TABLE files ADD COLUMN purpose TEXT NOT NULL DEFAULT '';

ALTER TABLE app_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_records FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON app_records USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE app_device_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_device_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON app_device_grants USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
ALTER TABLE files DROP COLUMN purpose;
DROP TABLE app_device_grants;
DROP TABLE app_records;
