-- +goose Up
-- Multilingual documents (ADR-0028): a project has a main language and
-- further languages; documents keep one version per further language.
ALTER TABLE projects ADD COLUMN main_locale TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN locales TEXT NOT NULL DEFAULT ''; -- comma-separated further languages
CREATE TABLE document_translations (
  document_id    TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  locale         TEXT NOT NULL,
  tenant_id      TEXT NOT NULL REFERENCES tenants(id),
  title          TEXT NOT NULL,
  content        JSONB NOT NULL,
  source_version INTEGER NOT NULL, -- version of the main language it was made from
  machine        BOOLEAN NOT NULL DEFAULT FALSE, -- made by an AI and not reviewed yet
  updated_by     TEXT NOT NULL,
  updated_at     TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (document_id, locale)
);
ALTER TABLE document_translations ENABLE ROW LEVEL SECURITY;
ALTER TABLE document_translations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON document_translations USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE document_translations;
ALTER TABLE projects DROP COLUMN locales;
ALTER TABLE projects DROP COLUMN main_locale;
