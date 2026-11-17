-- +goose Up
-- Public view of feedback portals: moderators release topics; visitors
-- vote, subscribe to news and comment (comments are moderated).
ALTER TABLE feedback_topics ADD COLUMN public BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE feedback_topics ADD COLUMN votes INTEGER NOT NULL DEFAULT 0;

CREATE TABLE feedback_votes (
  topic_id   TEXT NOT NULL REFERENCES feedback_topics(id) ON DELETE CASCADE,
  voter      TEXT NOT NULL,                -- user:<id> or a hash of the visitor's key
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (topic_id, voter)
);

CREATE TABLE feedback_subscriptions (
  id         TEXT NOT NULL PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  topic_id   TEXT NOT NULL REFERENCES feedback_topics(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL DEFAULT '',
  email      TEXT NOT NULL DEFAULT '',
  lang       TEXT NOT NULL DEFAULT 'en',
  token_hash TEXT NOT NULL,                -- unsubscribe link
  created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX feedback_subscriptions_topic ON feedback_subscriptions(topic_id);
CREATE UNIQUE INDEX feedback_subscriptions_token ON feedback_subscriptions(token_hash);

CREATE TABLE feedback_comments (
  id          TEXT NOT NULL PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  topic_id    TEXT NOT NULL REFERENCES feedback_topics(id) ON DELETE CASCADE,
  user_id     TEXT NOT NULL DEFAULT '',
  author_name TEXT NOT NULL,
  body        TEXT NOT NULL,
  status      TEXT NOT NULL,               -- pending | approved | rejected
  team        BOOLEAN NOT NULL DEFAULT FALSE,
  created_at  TIMESTAMPTZ NOT NULL,
  decided_by  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX feedback_comments_topic ON feedback_comments(topic_id, status);

ALTER TABLE feedback_votes ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_votes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON feedback_votes USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE feedback_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_subscriptions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON feedback_subscriptions USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE feedback_comments ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_comments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON feedback_comments USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE feedback_comments;
DROP TABLE feedback_subscriptions;
DROP TABLE feedback_votes;
ALTER TABLE feedback_topics DROP COLUMN votes;
ALTER TABLE feedback_topics DROP COLUMN public;
