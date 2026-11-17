-- +goose Up
-- The words of the search index per entry: search corrects typos against
-- them when a query finds nothing (also without AI).
CREATE TABLE search_terms (
  ref_id     TEXT NOT NULL,
  tenant_id  TEXT NOT NULL,
  project_id TEXT NOT NULL,
  term       TEXT NOT NULL,
  PRIMARY KEY (ref_id, term)
);
CREATE INDEX search_terms_project ON search_terms(project_id, term);

-- +goose Down
DROP TABLE search_terms;
