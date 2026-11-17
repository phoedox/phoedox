-- +goose Up
-- External trackers (GitHub, Gitea/Forgejo, GitLab, Jira) that feedback
-- topics are forwarded to, and the issues made from topics.
CREATE TABLE feedback_trackers (
  id          TEXT NOT NULL PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  kind        TEXT NOT NULL,               -- github | gitea | gitlab | jira
  url         TEXT NOT NULL,
  jira_project TEXT NOT NULL DEFAULT '',
  issue_type  TEXT NOT NULL DEFAULT '',
  username    TEXT NOT NULL DEFAULT '',    -- Jira account e-mail (basic auth)
  token_var   TEXT NOT NULL DEFAULT '',    -- secret of the project environment
  labels      TEXT NOT NULL DEFAULT '[]',
  created_by  TEXT NOT NULL,
  created_at  TEXT NOT NULL
);
CREATE INDEX feedback_trackers_project ON feedback_trackers(project_id);

CREATE TABLE feedback_forwards (
  id           TEXT NOT NULL PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  topic_id     TEXT NOT NULL REFERENCES feedback_topics(id) ON DELETE CASCADE,
  tracker_id   TEXT NOT NULL REFERENCES feedback_trackers(id) ON DELETE CASCADE,
  ref          TEXT NOT NULL,              -- issue number or Jira key
  url          TEXT NOT NULL,
  reports_sent INTEGER NOT NULL DEFAULT 0, -- reports the issue knows about
  closed       BOOLEAN NOT NULL DEFAULT FALSE,
  last_error   TEXT NOT NULL DEFAULT '',
  created_by   TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  checked_at   TEXT
);
CREATE INDEX feedback_forwards_topic ON feedback_forwards(topic_id);
CREATE UNIQUE INDEX feedback_forwards_tracker ON feedback_forwards(topic_id, tracker_id);

-- +goose Down
DROP TABLE feedback_forwards;
DROP TABLE feedback_trackers;
