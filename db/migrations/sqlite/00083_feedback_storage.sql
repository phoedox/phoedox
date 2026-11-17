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
  created_at  TEXT NOT NULL,
  expires_at  TEXT,
  revoked     BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX feedback_storage_lots_project ON feedback_storage_lots(project_id);

-- +goose Down
DROP TABLE feedback_storage_lots;
