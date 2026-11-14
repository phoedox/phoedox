-- +goose Up
-- Approval workflow: in projects that require approval, drafts are published
-- by owners after a review request.
ALTER TABLE projects ADD COLUMN approval INTEGER NOT NULL DEFAULT 0;
ALTER TABLE drafts ADD COLUMN review_by TEXT NOT NULL DEFAULT '';
ALTER TABLE drafts ADD COLUMN review_at TEXT;
ALTER TABLE drafts ADD COLUMN review_note TEXT NOT NULL DEFAULT '';

-- Rules per page: review interval and required reading.
CREATE TABLE page_rules (
  document_id      TEXT PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
  tenant_id        TEXT NOT NULL REFERENCES tenants(id),
  project_id       TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  review_days      INTEGER NOT NULL DEFAULT 0,
  reviewed_at      TEXT,
  reviewed_by      TEXT NOT NULL DEFAULT '',
  notified_at      TEXT,
  required_reading INTEGER NOT NULL DEFAULT 0,
  updated_by       TEXT NOT NULL,
  updated_at       TEXT NOT NULL
);
CREATE INDEX page_rules_project ON page_rules(project_id);

CREATE TABLE read_confirmations (
  document_id  TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  version      INTEGER NOT NULL,
  confirmed_at TEXT NOT NULL,
  PRIMARY KEY (document_id, user_id)
);

-- Maintenance plans: recurring tasks per entry.
CREATE TABLE maintenance_plans (
  id           TEXT PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  entity_id    TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
  board_id     TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  title        TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  every        INTEGER NOT NULL,
  unit         TEXT NOT NULL,
  lead_days    INTEGER NOT NULL DEFAULT 7,
  assignee     TEXT NOT NULL DEFAULT '',
  next_due     TEXT NOT NULL,
  open_task_id TEXT NOT NULL DEFAULT '',
  last_done    TEXT,
  created_by   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at   TEXT NOT NULL
);
CREATE INDEX maintenance_plans_entity ON maintenance_plans(entity_id);
CREATE INDEX maintenance_plans_due ON maintenance_plans(next_due);

-- +goose Down
DROP TABLE maintenance_plans;
DROP TABLE read_confirmations;
DROP TABLE page_rules;
ALTER TABLE drafts DROP COLUMN review_note;
ALTER TABLE drafts DROP COLUMN review_at;
ALTER TABLE drafts DROP COLUMN review_by;
ALTER TABLE projects DROP COLUMN approval;
