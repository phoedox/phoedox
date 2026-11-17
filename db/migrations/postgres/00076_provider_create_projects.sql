-- +goose Up
-- Customers decide whether admins of a service provider may create projects
-- in their organization (ADR-0073); off by default.
ALTER TABLE provider_links ADD COLUMN may_create_projects BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE provider_links DROP COLUMN may_create_projects;
