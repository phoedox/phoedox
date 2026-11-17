-- +goose Up
-- AI credits: lots of credit (bought or granted) per person or organization,
-- each with its own expiry; use beyond the monthly budget draws from them,
-- the lot that expires first first. ai_usage notes what credits paid.
CREATE TABLE ai_credit_lots (
  id          TEXT NOT NULL PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  owner_kind  TEXT NOT NULL,          -- user | org
  owner_id    TEXT NOT NULL,
  amount      INTEGER NOT NULL,       -- micro-dollars (in-app prices)
  remaining   INTEGER NOT NULL,
  expires_at  TEXT,                   -- NULL = never
  source      TEXT NOT NULL,          -- purchase | grant
  source_ref  TEXT NOT NULL DEFAULT '',
  note        TEXT NOT NULL DEFAULT '',
  created_by  TEXT NOT NULL,
  created_at  TEXT NOT NULL
);
CREATE INDEX ai_credit_lots_owner ON ai_credit_lots(owner_kind, owner_id);

ALTER TABLE ai_usage ADD COLUMN credit_micros INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE ai_usage DROP COLUMN credit_micros;
DROP TABLE ai_credit_lots;
