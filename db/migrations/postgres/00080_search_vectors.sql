-- +goose Up
-- Semantic search: one embedding per search index entry and model
-- (float32, normalised to unit length). Rebuilt by a background job.
CREATE TABLE search_vectors (
  ref_id     TEXT NOT NULL,
  model      TEXT NOT NULL,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  vec        BYTEA NOT NULL,
  PRIMARY KEY (ref_id, model)
);
CREATE INDEX search_vectors_project ON search_vectors(project_id, model);
ALTER TABLE search_vectors ENABLE ROW LEVEL SECURITY;
ALTER TABLE search_vectors FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON search_vectors USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE search_vectors;
