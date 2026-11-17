-- +goose Up
-- What each reader did in a block of a page: ticked steps, a vote in a
-- poll, answers in a quiz. One row per page, block and person; polls count
-- the votes of all rows of their block.
CREATE TABLE block_states (
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  block_key   TEXT NOT NULL,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  data        TEXT NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (document_id, block_key, user_id)
);
CREATE INDEX block_states_project ON block_states (project_id);

ALTER TABLE block_states ENABLE ROW LEVEL SECURITY;
ALTER TABLE block_states FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON block_states USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE block_states;
