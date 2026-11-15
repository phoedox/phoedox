-- +goose Up
-- Time spent on cards (jobs with time tracking for service providers).
CREATE TABLE task_time (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  task_id     TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  user_id     TEXT NOT NULL,
  day         TEXT NOT NULL,
  minutes     INTEGER NOT NULL,
  note        TEXT NOT NULL DEFAULT '',
  billable    INTEGER NOT NULL DEFAULT 1,
  actor       TEXT NOT NULL,
  created_at  TEXT NOT NULL
);
CREATE INDEX task_time_task ON task_time(task_id);
CREATE INDEX task_time_project_day ON task_time(project_id, day);

-- +goose Down
DROP TABLE task_time;
