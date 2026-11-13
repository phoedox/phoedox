-- +goose Up
-- Organizations with admins and members, groups (manual or from OIDC),
-- project access for groups, guests (memberships that expire), documents
-- that are restricted or shared with single people, and read-only links.
ALTER TABLE organizations ADD COLUMN ai_enabled INTEGER NOT NULL DEFAULT 1;
ALTER TABLE organizations ADD COLUMN quota_mb INTEGER NOT NULL DEFAULT 0;

CREATE TABLE org_members (
  org_id     TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  role       TEXT NOT NULL CHECK (role IN ('admin', 'member')),
  created_at TEXT NOT NULL,
  PRIMARY KEY (org_id, user_id)
);
CREATE INDEX org_members_user ON org_members (user_id);

CREATE TABLE groups (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  org_id     TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  oidc_group TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (org_id, name)
);

CREATE TABLE group_members (
  group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source   TEXT NOT NULL DEFAULT 'manual', -- manual | oidc
  PRIMARY KEY (group_id, user_id)
);
CREATE INDEX group_members_user ON group_members (user_id);

CREATE TABLE project_groups (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  group_id   TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  role       TEXT NOT NULL CHECK (role IN ('owner', 'editor', 'reader')),
  PRIMARY KEY (project_id, group_id)
);

ALTER TABLE project_members ADD COLUMN expires_at TEXT;

ALTER TABLE documents ADD COLUMN restricted INTEGER NOT NULL DEFAULT 0;

CREATE TABLE document_grants (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  user_id     TEXT REFERENCES users(id) ON DELETE CASCADE,
  group_id    TEXT REFERENCES groups(id) ON DELETE CASCADE,
  role        TEXT NOT NULL CHECK (role IN ('editor', 'reader')),
  created_at  TEXT NOT NULL,
  CHECK ((user_id IS NULL) <> (group_id IS NULL))
);
CREATE UNIQUE INDEX document_grants_user ON document_grants (document_id, user_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX document_grants_group ON document_grants (document_id, group_id) WHERE group_id IS NOT NULL;
CREATE INDEX document_grants_principal ON document_grants (user_id);

CREATE TABLE share_links (
  id           TEXT PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  document_id  TEXT REFERENCES documents(id) ON DELETE CASCADE,
  token_hash   TEXT NOT NULL UNIQUE,
  label        TEXT NOT NULL DEFAULT '',
  created_by   TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  expires_at   TEXT,
  revoked_at   TEXT,
  last_used_at TEXT
);
CREATE INDEX share_links_project ON share_links (project_id);

-- +goose Down
DROP TABLE share_links;
DROP TABLE document_grants;
ALTER TABLE documents DROP COLUMN restricted;
ALTER TABLE project_members DROP COLUMN expires_at;
DROP TABLE project_groups;
DROP TABLE group_members;
DROP TABLE groups;
DROP TABLE org_members;
ALTER TABLE organizations DROP COLUMN quota_mb;
ALTER TABLE organizations DROP COLUMN ai_enabled;
