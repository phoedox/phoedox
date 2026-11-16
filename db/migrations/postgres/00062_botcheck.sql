-- +goose Up
-- Bot protection (usage-prices-concept): one-time ids of challenges and
-- pass tokens, and the event log (kept for 7 days).
CREATE TABLE bot_used (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  until      TIMESTAMPTZ NOT NULL
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
  created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX bot_events_time ON bot_events(created_at);
ALTER TABLE bot_used ENABLE ROW LEVEL SECURITY;
ALTER TABLE bot_used FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON bot_used USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE bot_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE bot_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON bot_events USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE bot_events;
DROP TABLE bot_used;
