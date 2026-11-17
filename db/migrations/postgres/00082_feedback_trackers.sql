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
  created_at  TIMESTAMPTZ NOT NULL
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
  created_at   TIMESTAMPTZ NOT NULL,
  checked_at   TIMESTAMPTZ
);
CREATE INDEX feedback_forwards_topic ON feedback_forwards(topic_id);
CREATE UNIQUE INDEX feedback_forwards_tracker ON feedback_forwards(topic_id, tracker_id);
ALTER TABLE feedback_trackers ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_trackers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON feedback_trackers USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE feedback_forwards ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_forwards FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON feedback_forwards USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE feedback_forwards;
DROP TABLE feedback_trackers;
