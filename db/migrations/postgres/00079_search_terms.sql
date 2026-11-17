-- +goose Up
-- The words of the search index per entry: search corrects typos against
-- them when a query finds nothing (also without AI).
CREATE TABLE search_terms (
  ref_id     TEXT NOT NULL,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  term       TEXT NOT NULL,
  PRIMARY KEY (ref_id, term)
);
CREATE INDEX search_terms_project ON search_terms(project_id, term);
ALTER TABLE search_terms ENABLE ROW LEVEL SECURITY;
ALTER TABLE search_terms FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON search_terms USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE search_terms;
