-- +goose Up
-- Slugs of deleted entities become free again (needed for the trash).
ALTER TABLE entities DROP CONSTRAINT IF EXISTS entities_project_id_slug_key;
CREATE UNIQUE INDEX entities_slug ON entities (project_id, slug) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX entities_slug;
ALTER TABLE entities ADD CONSTRAINT entities_project_id_slug_key UNIQUE (project_id, slug);
