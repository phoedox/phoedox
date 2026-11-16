-- +goose Up
-- Usage and quotas (usage-prices-concept): who sees prices, how an included
-- budget is described, who is warned at 80 % and 100 %, and own token
-- prices per organization with the moment they take effect.
ALTER TABLE organizations ADD COLUMN ai_prices_hidden_admin BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE organizations ADD COLUMN ai_prices_hidden BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE organizations ADD COLUMN ai_included_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE organizations ADD COLUMN ai_warn TEXT NOT NULL DEFAULT 'admins';

ALTER TABLE ai_usage ADD COLUMN list_micros INTEGER NOT NULL DEFAULT 0;
UPDATE ai_usage SET list_micros = charge_micros;

CREATE TABLE org_ai_prices (
  id             TEXT PRIMARY KEY,
  tenant_id      TEXT NOT NULL REFERENCES tenants(id),
  org_id         TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  percent        REAL NOT NULL DEFAULT 0,
  models         TEXT NOT NULL DEFAULT '{}',
  mode           TEXT NOT NULL,
  effective_from TEXT NOT NULL,
  created_by     TEXT NOT NULL,
  created_at     TEXT NOT NULL
);
CREATE INDEX org_ai_prices_org ON org_ai_prices(org_id, effective_from);

CREATE TABLE budget_warnings (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  scope      TEXT NOT NULL,
  month      TEXT NOT NULL,
  level      INTEGER NOT NULL,
  sent_at    TEXT NOT NULL,
  PRIMARY KEY (tenant_id, scope, month, level)
);

-- +goose Down
DROP TABLE budget_warnings;
DROP TABLE org_ai_prices;
ALTER TABLE ai_usage DROP COLUMN list_micros;
ALTER TABLE organizations DROP COLUMN ai_warn;
ALTER TABLE organizations DROP COLUMN ai_included_tokens;
ALTER TABLE organizations DROP COLUMN ai_prices_hidden;
ALTER TABLE organizations DROP COLUMN ai_prices_hidden_admin;
