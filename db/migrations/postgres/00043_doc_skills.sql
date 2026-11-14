-- +goose Up
-- Pages marked as AI skills: instructions an AI client loads for matching tasks.
CREATE TABLE doc_skills (
  document_id TEXT PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  description TEXT NOT NULL,
  updated_by  TEXT NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX doc_skills_name ON doc_skills(project_id, name);
ALTER TABLE doc_skills ENABLE ROW LEVEL SECURITY;
ALTER TABLE doc_skills FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON doc_skills USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE doc_skills;
