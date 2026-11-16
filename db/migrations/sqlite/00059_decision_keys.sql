-- +goose Up
-- Own numbers for imported decisions (e.g. "D-640" from another system):
-- found by links, search and the API besides ADR-0574. Translations can be
-- marked as original text the user wrote in that language.
ALTER TABLE documents ADD COLUMN ext_ref TEXT NOT NULL DEFAULT '';
CREATE INDEX documents_ext_ref ON documents (project_id, lower(ext_ref)) WHERE ext_ref <> '';
ALTER TABLE document_translations ADD COLUMN original INTEGER NOT NULL DEFAULT 0;

-- +goose Down
DROP INDEX documents_ext_ref;
ALTER TABLE document_translations DROP COLUMN original;
ALTER TABLE documents DROP COLUMN ext_ref;
