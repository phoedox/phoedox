-- +goose Up
-- Own domains of feedback portals (verified with a DNS TXT record).
ALTER TABLE feedback_portals ADD COLUMN domain TEXT NOT NULL DEFAULT '';
ALTER TABLE feedback_portals ADD COLUMN domain_token TEXT NOT NULL DEFAULT '';
ALTER TABLE feedback_portals ADD COLUMN domain_verified_at TIMESTAMPTZ;
CREATE UNIQUE INDEX feedback_portals_domain ON feedback_portals(domain) WHERE domain <> '';

-- +goose Down
DROP INDEX feedback_portals_domain;
ALTER TABLE feedback_portals DROP COLUMN domain_verified_at;
ALTER TABLE feedback_portals DROP COLUMN domain_token;
ALTER TABLE feedback_portals DROP COLUMN domain;
