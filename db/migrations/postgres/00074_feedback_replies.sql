-- +goose Up
-- Feedback portals (feedback-concept): a key products use to sign who
-- reports and from which build (sealed), questions to reporters with their
-- answers, and automatic answers for issues the team already answered.
ALTER TABLE feedback_portals ADD COLUMN player_key_sealed TEXT NOT NULL DEFAULT '';
ALTER TABLE feedback_topics ADD COLUMN question TEXT NOT NULL DEFAULT '';
ALTER TABLE feedback_topics ADD COLUMN answer_auto BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE feedback_replies (
  id         TEXT NOT NULL PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  topic_id   TEXT NOT NULL REFERENCES feedback_topics(id) ON DELETE CASCADE,
  report_id  TEXT NOT NULL REFERENCES feedback_reports(id) ON DELETE CASCADE,
  body       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX feedback_replies_report ON feedback_replies(report_id);
ALTER TABLE feedback_replies ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_replies FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON feedback_replies USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE feedback_replies;
ALTER TABLE feedback_topics DROP COLUMN answer_auto;
ALTER TABLE feedback_topics DROP COLUMN question;
ALTER TABLE feedback_portals DROP COLUMN player_key_sealed;
