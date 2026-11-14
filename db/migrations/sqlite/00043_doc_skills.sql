-- +goose Up
-- Pages marked as AI skills: instructions an AI client loads for matching tasks.
CREATE TABLE doc_skills (
  document_id TEXT PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  description TEXT NOT NULL,
  updated_by  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE UNIQUE INDEX doc_skills_name ON doc_skills(project_id, name);

-- +goose Down
DROP TABLE doc_skills;
