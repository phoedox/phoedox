-- +goose Up
-- Entitlements (ADR-0061): which organization or user may use a paid
-- feature, and why. source is grandfathered (signed up before the feature
-- became paid; never removed), plan (billing) or grant (system admin).
-- subject is "org:<id>" or "user:<id>".
CREATE TABLE entitlements (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  subject    TEXT NOT NULL,
  feature    TEXT NOT NULL,
  source     TEXT NOT NULL,
  granted_at TEXT NOT NULL,
  granted_by TEXT NOT NULL DEFAULT '',
  note       TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (subject, feature)
);


-- +goose Down
DROP TABLE entitlements;
