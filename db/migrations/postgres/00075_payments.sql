-- +goose Up
-- Payments (ADR-0079): purchases made through the payment provider (Stripe,
-- classic or Managed Payments, or Paddle) and the provider's webhook events, stored
-- once so a repeated delivery changes nothing.
CREATE TABLE purchases (
  id               TEXT NOT NULL PRIMARY KEY,
  tenant_id        TEXT NOT NULL REFERENCES tenants(id),
  user_id          TEXT NOT NULL DEFAULT '',
  org_id           TEXT NOT NULL DEFAULT '',
  kind             TEXT NOT NULL,
  item             TEXT NOT NULL DEFAULT '',
  description      TEXT NOT NULL DEFAULT '',
  amount           BIGINT NOT NULL,
  currency         TEXT NOT NULL,
  recurring        TEXT NOT NULL DEFAULT '',
  status           TEXT NOT NULL,
  provider         TEXT NOT NULL,
  mode             TEXT NOT NULL,
  checkout_ref     TEXT NOT NULL DEFAULT '',
  payment_ref      TEXT NOT NULL DEFAULT '',
  subscription_ref TEXT NOT NULL DEFAULT '',
  customer_ref     TEXT NOT NULL DEFAULT '',
  return_path      TEXT NOT NULL DEFAULT '',
  created_at       TIMESTAMPTZ NOT NULL,
  updated_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX purchases_user ON purchases(user_id);
CREATE INDEX purchases_subscription ON purchases(subscription_ref);
CREATE INDEX purchases_checkout ON purchases(checkout_ref);

CREATE TABLE payment_events (
  provider    TEXT NOT NULL,
  event_id    TEXT NOT NULL,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  type        TEXT NOT NULL,
  original    TEXT NOT NULL,
  purchase_id TEXT NOT NULL DEFAULT '',
  received_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (provider, event_id)
);
ALTER TABLE purchases ENABLE ROW LEVEL SECURITY;
ALTER TABLE purchases FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON purchases USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE payment_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE payment_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payment_events USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE payment_events;
DROP TABLE purchases;
