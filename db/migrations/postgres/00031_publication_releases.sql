-- +goose Up
-- Fixed states of a publication (concept: "nur freigegebene Versionen"):
-- a release is a point in time; pages show their last version before it.
CREATE TABLE publication_releases (
  id             TEXT PRIMARY KEY,
  tenant_id      TEXT NOT NULL REFERENCES tenants(id),
  publication_id TEXT NOT NULL REFERENCES publications(id) ON DELETE CASCADE,
  number         INTEGER NOT NULL,
  note           TEXT NOT NULL DEFAULT '',
  created_by     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at     TIMESTAMPTZ NOT NULL,
  UNIQUE (publication_id, number)
);
ALTER TABLE publication_releases ENABLE ROW LEVEL SECURITY;
ALTER TABLE publication_releases FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON publication_releases USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE publication_releases;
