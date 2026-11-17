-- +goose Up
-- Drafts tied to a pull request or branch of the project's code connection:
-- published when the pull request is merged, discarded when it is closed.
ALTER TABLE drafts ADD COLUMN code_repo TEXT NOT NULL DEFAULT '';
ALTER TABLE drafts ADD COLUMN code_pr INTEGER NOT NULL DEFAULT 0;
ALTER TABLE drafts ADD COLUMN code_branch TEXT NOT NULL DEFAULT '';
-- The repository's webhook secret (sealed) for pull request events.
ALTER TABLE code_repos ADD COLUMN webhook_secret TEXT NOT NULL DEFAULT '';
-- Publications of chosen pages only (document ids; empty = all pages).
ALTER TABLE publications ADD COLUMN pages TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE publications DROP COLUMN pages;
ALTER TABLE code_repos DROP COLUMN webhook_secret;
ALTER TABLE drafts DROP COLUMN code_branch;
ALTER TABLE drafts DROP COLUMN code_pr;
ALTER TABLE drafts DROP COLUMN code_repo;
