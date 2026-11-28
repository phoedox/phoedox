-- +goose Up
-- Options of a plugin in a project (contributes.settings), set by the owners.
ALTER TABLE project_plugins ADD COLUMN settings TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE project_plugins DROP COLUMN settings;
