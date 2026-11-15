-- +goose Up
-- In-app prices per model (ADR-0069): the ledger keeps the provider's cost
-- and the amount charged against budgets (and later credits).
ALTER TABLE ai_usage ADD COLUMN charge_micros BIGINT NOT NULL DEFAULT 0;
UPDATE ai_usage SET charge_micros = cost_micros;

-- +goose Down
ALTER TABLE ai_usage DROP COLUMN charge_micros;
