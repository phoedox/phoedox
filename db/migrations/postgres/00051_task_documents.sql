-- +goose Up
-- Pages, decisions and other documents linked to a task card (ADR-0064);
-- several per card, in the order they were added.
CREATE TABLE task_documents (
  task_id     TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  position    INTEGER NOT NULL DEFAULT 0,
  created_by  TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (task_id, document_id)
);
CREATE INDEX task_documents_document ON task_documents(document_id);
CREATE INDEX task_documents_project ON task_documents(project_id);
ALTER TABLE task_documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_documents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON task_documents USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE task_documents;
