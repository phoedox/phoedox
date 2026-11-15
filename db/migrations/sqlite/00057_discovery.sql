-- +goose Up
-- Devices an agent saw in its network (neighbour table, mDNS). People add
-- them as entries or ignore them. Organizations can forbid discovery.
CREATE TABLE discovered_devices (
  id           TEXT PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id     TEXT NOT NULL,
  key          TEXT NOT NULL,
  ip           TEXT NOT NULL DEFAULT '',
  mac          TEXT NOT NULL DEFAULT '',
  hostname     TEXT NOT NULL DEFAULT '',
  services     TEXT NOT NULL DEFAULT '',
  via          TEXT NOT NULL DEFAULT '',
  first_seen   TEXT NOT NULL,
  last_seen    TEXT NOT NULL,
  dismissed_at TEXT,
  UNIQUE (project_id, key)
);
ALTER TABLE organizations ADD COLUMN discovery_off INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE organizations DROP COLUMN discovery_off;
DROP TABLE discovered_devices;
