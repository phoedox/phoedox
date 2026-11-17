-- +goose Up
-- Feedback portals, more (feedback-concept): the report count when a card
-- was linked (cards show "+n new reports"), the version a topic was fixed
-- in, and topics linked to knowledge-graph entries (by moderators or
-- suggested by the AI triage).
ALTER TABLE feedback_topic_links ADD COLUMN reports_at_link INTEGER NOT NULL DEFAULT 0;
ALTER TABLE feedback_topics ADD COLUMN fixed_in TEXT NOT NULL DEFAULT '';

CREATE TABLE feedback_topic_entities (
  topic_id   TEXT NOT NULL REFERENCES feedback_topics(id) ON DELETE CASCADE,
  entity_id  TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  source     TEXT NOT NULL DEFAULT 'person',
  created_at TEXT NOT NULL,
  PRIMARY KEY (topic_id, entity_id)
);
CREATE INDEX feedback_topic_entities_entity ON feedback_topic_entities(entity_id);

-- +goose Down
DROP TABLE feedback_topic_entities;
ALTER TABLE feedback_topics DROP COLUMN fixed_in;
ALTER TABLE feedback_topic_links DROP COLUMN reports_at_link;
