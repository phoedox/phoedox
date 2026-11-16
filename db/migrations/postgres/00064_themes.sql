-- +goose Up
-- UI themes ([[ui-themes-concept]]): token sets for light and dark,
-- fonts, shape and optional own CSS (released per Phoedox version), owned
-- by a person, a project, an organization or the instance.
CREATE TABLE themes (
  id           TEXT PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  scope        TEXT NOT NULL,
  owner_id     TEXT NOT NULL DEFAULT '',
  name         TEXT NOT NULL,
  data         TEXT NOT NULL DEFAULT '{}',
  css          TEXT NOT NULL DEFAULT '',
  css_released TEXT NOT NULL DEFAULT '',
  created_by   TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL,
  updated_at   TIMESTAMPTZ NOT NULL
);
CREATE INDEX themes_owner ON themes(scope, owner_id);

-- Choices: a person's theme ("always mine" and the contrast mode), a
-- project's theme, an organization's default (forced or not).
ALTER TABLE users ADD COLUMN theme_id TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN theme_always BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN theme_contrast TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN theme_id TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN theme_id TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN theme_forced BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE themes ENABLE ROW LEVEL SECURITY;
ALTER TABLE themes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON themes USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
ALTER TABLE organizations DROP COLUMN theme_forced;
ALTER TABLE organizations DROP COLUMN theme_id;
ALTER TABLE projects DROP COLUMN theme_id;
ALTER TABLE users DROP COLUMN theme_contrast;
ALTER TABLE users DROP COLUMN theme_always;
ALTER TABLE users DROP COLUMN theme_id;
DROP TABLE themes;
