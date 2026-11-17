-- +goose Up
-- The app on an organization's own domain needs a system admin's switch;
-- organizations that already have a domain keep it.
ALTER TABLE organizations ADD COLUMN app_domain_allowed BOOLEAN NOT NULL DEFAULT 0;
UPDATE organizations SET app_domain_allowed = TRUE WHERE id IN (SELECT org_id FROM org_domains);

-- +goose Down
ALTER TABLE organizations DROP COLUMN app_domain_allowed;
