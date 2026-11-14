-- +goose Up
-- Audit log per project: events remember the project they belong to.
ALTER TABLE events ADD COLUMN project_id TEXT;
CREATE INDEX events_project ON events (project_id, id);

-- +goose Down
DROP INDEX events_project;
ALTER TABLE events DROP COLUMN project_id;
