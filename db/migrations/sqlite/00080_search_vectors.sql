-- +goose Up
-- Semantic search: one embedding per search index entry and model
-- (float32, normalised to unit length). Rebuilt by a background job.
CREATE TABLE search_vectors (
  ref_id     TEXT NOT NULL,
  model      TEXT NOT NULL,
  tenant_id  TEXT NOT NULL,
  project_id TEXT NOT NULL,
  vec        BLOB NOT NULL,
  PRIMARY KEY (ref_id, model)
);
CREATE INDEX search_vectors_project ON search_vectors(project_id, model);

-- +goose Down
DROP TABLE search_vectors;
