-- +goose Up
-- Devices (entries a control tool acts on): AI control per device and
-- people the owner lets switch a single device (ADR-0050).
CREATE TABLE device_settings (
  entity_id  TEXT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  ai_control TEXT NOT NULL DEFAULT 'suggest',
  updated_by TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE control_grants (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  entity_id  TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  granted_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT
);
CREATE UNIQUE INDEX control_grants_entity_user ON control_grants(entity_id, user_id);
CREATE INDEX control_grants_user ON control_grants(user_id);

-- +goose Down
DROP TABLE control_grants;
DROP TABLE device_settings;
