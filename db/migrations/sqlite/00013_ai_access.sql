-- +goose Up
-- Finer access for tokens and OAuth grants: read or write, all or some
-- projects (comma-separated ids, empty = all). Instance settings hold the
-- switch for AI access.
ALTER TABLE api_tokens ADD COLUMN access TEXT NOT NULL DEFAULT 'write';
ALTER TABLE api_tokens ADD COLUMN projects TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_grants ADD COLUMN access TEXT NOT NULL DEFAULT 'write';
ALTER TABLE oauth_grants ADD COLUMN projects TEXT NOT NULL DEFAULT '';

CREATE TABLE instance_settings (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  key        TEXT NOT NULL,
  value      TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (tenant_id, key)
);

-- +goose Down
DROP TABLE instance_settings;
ALTER TABLE oauth_grants DROP COLUMN projects;
ALTER TABLE oauth_grants DROP COLUMN access;
ALTER TABLE api_tokens DROP COLUMN projects;
ALTER TABLE api_tokens DROP COLUMN access;
