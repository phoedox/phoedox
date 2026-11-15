-- +goose Up
-- Automation rules (ADR-0060): when something happens in a project (a check
-- fails or recovers, an entry is created, a date such as "cancel by" comes
-- close), do something (create a task, notify people, send a request, e.g.
-- to open a ticket). trigger and action are JSON.
CREATE TABLE automation_rules (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  enabled     BOOLEAN NOT NULL DEFAULT true,
  trigger_def TEXT NOT NULL,
  action_def  TEXT NOT NULL,
  created_by  TEXT NOT NULL REFERENCES users(id),
  created_at  TIMESTAMPTZ NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL,
  last_run_at TIMESTAMPTZ,
  last_status TEXT NOT NULL DEFAULT '',
  last_error  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX automation_rules_project ON automation_rules(project_id);
ALTER TABLE automation_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE automation_rules FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON automation_rules USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- Runs remember what a rule already handled, so the same event or date
-- does not fire twice.
CREATE TABLE rule_runs (
  rule_id    TEXT NOT NULL REFERENCES automation_rules(id) ON DELETE CASCADE,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  run_key    TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (rule_id, run_key)
);
ALTER TABLE rule_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE rule_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON rule_runs USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE rule_runs;
DROP TABLE automation_rules;
