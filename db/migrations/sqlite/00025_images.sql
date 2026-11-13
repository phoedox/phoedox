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
  data       BLOB NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (kind, owner_id)
);

-- +goose Down
DROP TABLE images;
ALTER TABLE projects DROP COLUMN image;
ALTER TABLE users DROP COLUMN avatar;
