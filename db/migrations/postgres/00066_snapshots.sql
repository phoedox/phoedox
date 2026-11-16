-- +goose Up
-- Documentation snapshots: named points in a project's history (e.g. the
-- docs as they were at release 0.27.4), made by hand or from release tags
-- of the project's code connection.
CREATE TABLE project_snapshots (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  note        TEXT NOT NULL DEFAULT '',
  at          TIMESTAMPTZ NOT NULL,
  source      TEXT NOT NULL DEFAULT 'manual',
  source_ref  TEXT NOT NULL DEFAULT '',
  created_by  TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL,
  UNIQUE (project_id, name)
);
ALTER TABLE project_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_snapshots FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON project_snapshots USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE project_snapshots;
