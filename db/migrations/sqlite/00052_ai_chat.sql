-- +goose Up
-- AI chat (ADR-0066): chats per user and project, their messages as the
-- provider's own JSON (append-only, so reasoning stays valid), confirmed or
-- rejected card proposals, and a usage ledger for budgets. Organizations get
-- a chat switch and budgets in cents per month (NULL = instance default).
CREATE TABLE ai_chats (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  document_id TEXT REFERENCES documents(id) ON DELETE SET NULL,
  title       TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE INDEX ai_chats_user ON ai_chats(user_id, project_id, updated_at);

CREATE TABLE ai_chat_messages (
  chat_id     TEXT NOT NULL REFERENCES ai_chats(id) ON DELETE CASCADE,
  seq         INTEGER NOT NULL,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  role        TEXT NOT NULL,
  content     TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  PRIMARY KEY (chat_id, seq)
);

CREATE TABLE ai_chat_proposals (
  chat_id     TEXT NOT NULL REFERENCES ai_chats(id) ON DELETE CASCADE,
  tool_use_id TEXT NOT NULL,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  status      TEXT NOT NULL,
  result      TEXT NOT NULL DEFAULT '',
  decided_by  TEXT NOT NULL,
  decided_at  TEXT NOT NULL,
  PRIMARY KEY (chat_id, tool_use_id)
);

CREATE TABLE ai_usage (
  id            TEXT PRIMARY KEY,
  tenant_id     TEXT NOT NULL REFERENCES tenants(id),
  org_id        TEXT NOT NULL DEFAULT '',
  user_id       TEXT NOT NULL,
  project_id    TEXT NOT NULL DEFAULT '',
  chat_id       TEXT NOT NULL DEFAULT '',
  model         TEXT NOT NULL,
  input_tokens  INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cache_read    INTEGER NOT NULL DEFAULT 0,
  cache_write   INTEGER NOT NULL DEFAULT 0,
  cost_micros   INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT NOT NULL
);
CREATE INDEX ai_usage_org ON ai_usage(org_id, created_at);
CREATE INDEX ai_usage_user ON ai_usage(user_id, created_at);

ALTER TABLE organizations ADD COLUMN ai_chat_enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE organizations ADD COLUMN ai_chat_budget_cents INTEGER;
ALTER TABLE organizations ADD COLUMN ai_chat_cap_cents INTEGER;
ALTER TABLE organizations ADD COLUMN ai_chat_user_budget_cents INTEGER;

-- +goose Down
DROP TABLE ai_usage;
DROP TABLE ai_chat_proposals;
DROP TABLE ai_chat_messages;
DROP TABLE ai_chats;
ALTER TABLE organizations DROP COLUMN ai_chat_user_budget_cents;
ALTER TABLE organizations DROP COLUMN ai_chat_cap_cents;
ALTER TABLE organizations DROP COLUMN ai_chat_budget_cents;
ALTER TABLE organizations DROP COLUMN ai_chat_enabled;
