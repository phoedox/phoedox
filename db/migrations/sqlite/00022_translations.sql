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
  content        TEXT NOT NULL,
  source_version INTEGER NOT NULL, -- version of the main language it was made from
  machine        INTEGER NOT NULL DEFAULT 0, -- made by an AI and not reviewed yet
  updated_by     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  PRIMARY KEY (document_id, locale)
);

-- +goose Down
DROP TABLE document_translations;
ALTER TABLE projects DROP COLUMN locales;
ALTER TABLE projects DROP COLUMN main_locale;
