-- +goose Up
-- Usage and quotas (usage-prices-concept): who sees prices, how an included
-- budget is described, who is warned at 80 % and 100 %, and own token
-- prices per organization with the moment they take effect.

-- Set by system admins; organization admins cannot show prices again.
ALTER TABLE organizations ADD COLUMN ai_prices_hidden_admin BOOLEAN NOT NULL DEFAULT FALSE;
-- Set by organization admins for their projects and members.
ALTER TABLE organizations ADD COLUMN ai_prices_hidden BOOLEAN NOT NULL DEFAULT FALSE;
-- Included budget stated in tokens (0 = described as an amount).
ALTER TABLE organizations ADD COLUMN ai_included_tokens BIGINT NOT NULL DEFAULT 0;
-- Who is warned at 80 % and 100 % of the organization's budget:
-- comma-separated list of admins, members, owners.
ALTER TABLE organizations ADD COLUMN ai_warn TEXT NOT NULL DEFAULT 'admins';

-- The in-app price before an organization's own prices (list price), so a
-- month can be recalculated when those prices change.
ALTER TABLE ai_usage ADD COLUMN list_micros BIGINT NOT NULL DEFAULT 0;
UPDATE ai_usage SET list_micros = charge_micros;

CREATE TABLE org_ai_prices (
  id             TEXT PRIMARY KEY,
  tenant_id      TEXT NOT NULL REFERENCES tenants(id),
  org_id         TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  percent        DOUBLE PRECISION NOT NULL DEFAULT 0,
  models         TEXT NOT NULL DEFAULT '{}',
  mode           TEXT NOT NULL,
  effective_from TIMESTAMPTZ NOT NULL,
  created_by     TEXT NOT NULL,
  created_at     TIMESTAMPTZ NOT NULL
);
CREATE INDEX org_ai_prices_org ON org_ai_prices(org_id, effective_from);
ALTER TABLE org_ai_prices ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_ai_prices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON org_ai_prices USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- Budget warnings already sent (one per budget, month and level).
CREATE TABLE budget_warnings (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  scope      TEXT NOT NULL,
  month      TEXT NOT NULL,
  level      INTEGER NOT NULL,
  sent_at    TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, scope, month, level)
);
ALTER TABLE budget_warnings ENABLE ROW LEVEL SECURITY;
ALTER TABLE budget_warnings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON budget_warnings USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE budget_warnings;
DROP TABLE org_ai_prices;
ALTER TABLE ai_usage DROP COLUMN list_micros;
ALTER TABLE organizations DROP COLUMN ai_warn;
ALTER TABLE organizations DROP COLUMN ai_included_tokens;
ALTER TABLE organizations DROP COLUMN ai_prices_hidden;
ALTER TABLE organizations DROP COLUMN ai_prices_hidden_admin;
