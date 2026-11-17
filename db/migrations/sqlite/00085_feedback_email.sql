-- +goose Up
-- Reports by e-mail: a portal's inbound address key (hashed).
ALTER TABLE feedback_portals ADD COLUMN email_key_hash TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE feedback_portals DROP COLUMN email_key_hash;
