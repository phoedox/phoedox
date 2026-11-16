-- +goose Up
-- Bot protection (usage-prices-concept): one-time ids of challenges and
-- pass tokens, and the event log (kept for 7 days).
CREATE TABLE bot_used (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  until      TEXT NOT NULL
);
CREATE TABLE bot_events (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  place      TEXT NOT NULL,
  policy     TEXT NOT NULL,
  outcome    TEXT NOT NULL,
  reason     TEXT NOT NULL DEFAULT '',
  ip_hash    TEXT NOT NULL DEFAULT '',
  solve_ms   INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE INDEX bot_events_time ON bot_events(created_at);

-- +goose Down
DROP TABLE bot_events;
DROP TABLE bot_used;
