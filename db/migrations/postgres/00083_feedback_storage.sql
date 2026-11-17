-- +goose Up
-- Extra feedback storage for a project: bought packs or grants, each for a
-- time (on top of the project's feedback storage limit).
CREATE TABLE feedback_storage_lots (
  id          TEXT NOT NULL PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  mb          INTEGER NOT NULL,
  source      TEXT NOT NULL,               -- purchase | grant
  source_ref  TEXT NOT NULL DEFAULT '',
  note        TEXT NOT NULL DEFAULT '',
  created_by  TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL,
  expires_at  TIMESTAMPTZ,
  revoked     BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX feedback_storage_lots_project ON feedback_storage_lots(project_id);
ALTER TABLE feedback_storage_lots ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_storage_lots FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON feedback_storage_lots USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE feedback_storage_lots;
