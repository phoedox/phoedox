-- +goose Up
-- Task boards (ADR-0030): a board is a document of type "board" with its
-- columns; tasks are its cards.
CREATE TABLE tasks (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  board_id    TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  column_id   TEXT NOT NULL,
  position    REAL NOT NULL DEFAULT 0,
  title       TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  assignee_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  due         TEXT NOT NULL DEFAULT '', -- YYYY-MM-DD
  priority    INTEGER NOT NULL DEFAULT 0, -- 0 none, 1 low, 2 medium, 3 high
  labels      TEXT NOT NULL DEFAULT '', -- comma-separated
  created_by  TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  done_at     TEXT
);
CREATE INDEX tasks_board ON tasks (board_id, column_id, position);
CREATE INDEX tasks_project ON tasks (project_id);
CREATE INDEX tasks_assignee ON tasks (assignee_id);

-- +goose Down
DROP TABLE tasks;
