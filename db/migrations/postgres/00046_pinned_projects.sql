-- +goose Up
-- Projects a person pinned on the projects page (JSON list of project ids).
ALTER TABLE user_prefs ADD COLUMN pinned_projects TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE user_prefs DROP COLUMN pinned_projects;
