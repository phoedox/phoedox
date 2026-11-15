-- +goose Up
-- Deleted projects stay in the trash (deleted_at) for the trash period; who deleted them is kept for the restore list.
ALTER TABLE projects ADD COLUMN deleted_by TEXT;

-- +goose Down
ALTER TABLE projects DROP COLUMN deleted_by;
