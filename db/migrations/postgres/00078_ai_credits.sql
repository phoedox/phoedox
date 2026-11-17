-- +goose Up
-- AI credits: lots of credit (bought or granted) per person or organization,
-- each with its own expiry; use beyond the monthly budget draws from them,
-- the lot that expires first first. ai_usage notes what credits paid.
CREATE TABLE ai_credit_lots (
  id          TEXT NOT NULL PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  owner_kind  TEXT NOT NULL,          -- user | org
  owner_id    TEXT NOT NULL,
  amount      BIGINT NOT NULL,       -- micro-dollars (in-app prices)
  remaining   BIGINT NOT NULL,
  expires_at  TIMESTAMPTZ,                 -- NULL = never
  source      TEXT NOT NULL,          -- purchase | grant
  source_ref  TEXT NOT NULL DEFAULT '',
  note        TEXT NOT NULL DEFAULT '',
  created_by  TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX ai_credit_lots_owner ON ai_credit_lots(owner_kind, owner_id);

ALTER TABLE ai_usage ADD COLUMN credit_micros BIGINT NOT NULL DEFAULT 0;
ALTER TABLE ai_credit_lots ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_credit_lots FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ai_credit_lots USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
ALTER TABLE ai_usage DROP COLUMN credit_micros;
DROP TABLE ai_credit_lots;
