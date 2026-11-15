-- +goose Up
-- Subtasks of a card: a checklist as JSON ([{id, title, done, due}]).
ALTER TABLE tasks ADD COLUMN subtasks TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE tasks DROP COLUMN subtasks;
