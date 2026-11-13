-- +goose Up
-- Project kinds: custom kinds (full definition) or extensions of core kinds
-- (extra attributes and relations). Core kinds ship with Phoedox.
CREATE TABLE project_kinds (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  def        TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (project_id, kind)
);

-- +goose Down
DROP TABLE project_kinds;
