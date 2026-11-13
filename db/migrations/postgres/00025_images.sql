-- +goose Up
-- Profile pictures and project images (small, resized in the browser).
-- users.avatar / projects.image hold a version for cache busting ("" = none).
ALTER TABLE users ADD COLUMN avatar TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN image TEXT NOT NULL DEFAULT '';
CREATE TABLE images (
  kind       TEXT NOT NULL, -- user | project
  owner_id   TEXT NOT NULL,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  mime       TEXT NOT NULL,
  data       BYTEA NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (kind, owner_id)
);
ALTER TABLE images ENABLE ROW LEVEL SECURITY;
ALTER TABLE images FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON images USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE images;
ALTER TABLE projects DROP COLUMN image;
ALTER TABLE users DROP COLUMN avatar;
