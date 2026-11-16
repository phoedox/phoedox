-- +goose Up
-- Feedback portals ([[feedback-concept]], ADR-0075): portals per project,
-- reports from people (signed in or anonymous), topics that group them,
-- attachments in their own feedback storage quota, and tickets (cards)
-- made from topics.
CREATE TABLE feedback_portals (
  id              TEXT PRIMARY KEY,
  tenant_id       TEXT NOT NULL REFERENCES tenants(id),
  project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  handle          TEXT NOT NULL UNIQUE,
  name            TEXT NOT NULL,
  enabled         BOOLEAN NOT NULL DEFAULT TRUE,
  settings        TEXT NOT NULL DEFAULT '{}',
  public_key      TEXT NOT NULL DEFAULT '',
  server_key_hash TEXT NOT NULL DEFAULT '',
  created_by      TEXT NOT NULL,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL
);
CREATE INDEX feedback_portals_project ON feedback_portals(project_id);

CREATE TABLE feedback_moderators (
  portal_id  TEXT NOT NULL REFERENCES feedback_portals(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  created_at TEXT NOT NULL,
  PRIMARY KEY (portal_id, user_id)
);

CREATE TABLE feedback_topics (
  id           TEXT PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  portal_id    TEXT NOT NULL REFERENCES feedback_portals(id) ON DELETE CASCADE,
  title        TEXT NOT NULL,
  summary      TEXT NOT NULL DEFAULT '',
  type         TEXT NOT NULL DEFAULT 'bug',
  category     TEXT NOT NULL DEFAULT '',
  tags         TEXT NOT NULL DEFAULT '[]',
  priority     INTEGER NOT NULL DEFAULT 3,
  state        TEXT NOT NULL,
  answer       TEXT NOT NULL DEFAULT '',
  report_count INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  closed_at    TEXT
);
CREATE INDEX feedback_topics_portal ON feedback_topics(portal_id, state);

CREATE TABLE feedback_reports (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  portal_id   TEXT NOT NULL REFERENCES feedback_portals(id) ON DELETE CASCADE,
  topic_id    TEXT REFERENCES feedback_topics(id) ON DELETE SET NULL,
  type        TEXT NOT NULL,
  title       TEXT NOT NULL,
  body        TEXT NOT NULL DEFAULT '',
  fields      TEXT NOT NULL DEFAULT '{}',
  context     TEXT NOT NULL DEFAULT '{}',
  lang        TEXT NOT NULL DEFAULT '',
  user_id     TEXT NOT NULL DEFAULT '',
  email       TEXT NOT NULL DEFAULT '',
  status_hash TEXT NOT NULL DEFAULT '',
  via         TEXT NOT NULL,
  check_kind  TEXT NOT NULL DEFAULT '',
  spam        BOOLEAN NOT NULL DEFAULT FALSE,
  deleted     BOOLEAN NOT NULL DEFAULT FALSE,
  created_at  TEXT NOT NULL
);
CREATE INDEX feedback_reports_topic ON feedback_reports(topic_id);
CREATE INDEX feedback_reports_status ON feedback_reports(status_hash);
CREATE INDEX feedback_reports_user ON feedback_reports(user_id);

CREATE TABLE feedback_attachments (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  report_id  TEXT NOT NULL REFERENCES feedback_reports(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  mime       TEXT NOT NULL,
  size       INTEGER NOT NULL,
  sha256     TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX feedback_attachments_report ON feedback_attachments(report_id);
CREATE INDEX feedback_attachments_project ON feedback_attachments(project_id);

CREATE TABLE feedback_topic_links (
  topic_id   TEXT NOT NULL REFERENCES feedback_topics(id) ON DELETE CASCADE,
  task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (topic_id, task_id)
);
CREATE INDEX feedback_topic_links_task ON feedback_topic_links(task_id);

-- Feedback storage per project in MB (NULL = the instance default).
ALTER TABLE projects ADD COLUMN feedback_quota_mb INTEGER;

-- +goose Down
ALTER TABLE projects DROP COLUMN feedback_quota_mb;
DROP TABLE feedback_topic_links;
DROP TABLE feedback_attachments;
DROP TABLE feedback_reports;
DROP TABLE feedback_topics;
DROP TABLE feedback_moderators;
DROP TABLE feedback_portals;
