-- +goose Up
-- Decisions (ADRs) are numbered per project. Numbers are never reused, also
-- not after deleting a decision, so the counter lives on the project.
ALTER TABLE documents ADD COLUMN number INTEGER;
ALTER TABLE projects ADD COLUMN adr_seq INTEGER NOT NULL DEFAULT 0;

UPDATE documents SET number = (
  SELECT COUNT(*) FROM documents d2
  WHERE d2.project_id = documents.project_id AND d2.doc_type = 'adr'
    AND (d2.created_at < documents.created_at OR (d2.created_at = documents.created_at AND d2.id <= documents.id))
) WHERE doc_type = 'adr';

UPDATE projects SET adr_seq = (
  SELECT COALESCE(MAX(number), 0) FROM documents WHERE documents.project_id = projects.id
);

CREATE UNIQUE INDEX documents_number ON documents (project_id, number) WHERE number IS NOT NULL;

-- +goose Down
DROP INDEX documents_number;
ALTER TABLE projects DROP COLUMN adr_seq;
ALTER TABLE documents DROP COLUMN number;
