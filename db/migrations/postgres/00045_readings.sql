-- +goose Up
-- Measured values of entries over time (sensors, switch states), from
-- plugins, the agent or the API (ADR-0051). Kept for 90 days.
CREATE TABLE entity_readings (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  entity_id  TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
  metric     TEXT NOT NULL,
  value      DOUBLE PRECISION NOT NULL,
  unit       TEXT NOT NULL DEFAULT '',
  at         TIMESTAMPTZ NOT NULL
);
CREATE INDEX entity_readings_series ON entity_readings(entity_id, metric, at);
CREATE INDEX entity_readings_at ON entity_readings(at);
ALTER TABLE entity_readings ENABLE ROW LEVEL SECURITY;
ALTER TABLE entity_readings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON entity_readings USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE entity_readings;
