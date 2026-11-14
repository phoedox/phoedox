-- +goose Up
-- Free-text assignees on cards (e.g. "Claude") and boards that belong to a
-- page instead of the sidebar (parent_id).
ALTER TABLE tasks ADD COLUMN assignee_text TEXT NOT NULL DEFAULT '';
ALTER TABLE documents ADD COLUMN parent_id TEXT REFERENCES documents(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE documents DROP COLUMN parent_id;
ALTER TABLE tasks DROP COLUMN assignee_text;
