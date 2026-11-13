-- +goose Up
-- Project environment for request blocks: variables and secrets. Secret
-- values are encrypted with the instance master key and never returned.
CREATE TABLE project_env (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  value      TEXT NOT NULL,
  secret     INTEGER NOT NULL DEFAULT 0,
  updated_by TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (project_id, name)
);

-- +goose Down
DROP TABLE project_env;
