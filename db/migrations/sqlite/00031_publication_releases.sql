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
  created_at     TEXT NOT NULL,
  UNIQUE (publication_id, number)
);

-- +goose Down
DROP TABLE publication_releases;
