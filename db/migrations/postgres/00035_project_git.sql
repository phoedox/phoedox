-- +goose Up
-- Git sync of a project: the readable export (docs/ as Markdown plus the
-- JSON files) is committed to a folder of a Git repository. The token is
-- a secret of the project environment, named by token_var.
CREATE TABLE project_git (
  project_id  TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  url         TEXT NOT NULL,
  branch      TEXT NOT NULL DEFAULT 'main',
  folder      TEXT NOT NULL DEFAULT 'phoedox',
  token_var   TEXT NOT NULL DEFAULT '',
  enabled     BOOLEAN NOT NULL DEFAULT false,
  last_sync   TIMESTAMPTZ,
  last_commit TEXT NOT NULL DEFAULT '',
  last_error  TEXT NOT NULL DEFAULT '',
  updated_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  updated_at  TIMESTAMPTZ NOT NULL
);
ALTER TABLE project_git ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_git FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON project_git USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE project_git;
