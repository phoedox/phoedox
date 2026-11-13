-- +goose Up
-- Webhooks per project: signed, versioned event deliveries with a log.
CREATE TABLE webhooks (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  url        TEXT NOT NULL,
  secret     TEXT NOT NULL, -- sealed with the master key
  events     TEXT NOT NULL DEFAULT '*',
  active     INTEGER NOT NULL DEFAULT 1,
  failures   INTEGER NOT NULL DEFAULT 0,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX webhooks_project ON webhooks (project_id);

CREATE TABLE webhook_deliveries (
  id           TEXT PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  webhook_id   TEXT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
  event_type   TEXT NOT NULL,
  body         TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ok', 'failed')),
  response     INTEGER NOT NULL DEFAULT 0,
  error        TEXT NOT NULL DEFAULT '',
  attempts     INTEGER NOT NULL DEFAULT 0,
  duration_ms  INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT NOT NULL,
  delivered_at TEXT
);
CREATE INDEX webhook_deliveries_hook ON webhook_deliveries (webhook_id, created_at);

-- +goose Down
DROP TABLE webhook_deliveries;
DROP TABLE webhooks;
