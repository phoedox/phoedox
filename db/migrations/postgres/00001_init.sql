-- +goose Up
-- Core schema. tenant_id is present everywhere; in single-tenant mode it is a
-- fixed value. In the hosted variant row-level security enforces isolation.
CREATE TABLE tenants (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE organizations (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  name       TEXT NOT NULL,
  slug       TEXT NOT NULL,
  usage_type TEXT NOT NULL CHECK (usage_type IN ('commercial', 'noncommercial')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, slug)
);

CREATE TABLE users (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  email      TEXT NOT NULL,
  name       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, email)
);

CREATE TABLE projects (
  id            TEXT PRIMARY KEY,
  tenant_id     TEXT NOT NULL REFERENCES tenants(id),
  org_id        TEXT REFERENCES organizations(id),   -- NULL = personal space
  owner_id      TEXT NOT NULL REFERENCES users(id),
  slug          TEXT NOT NULL,
  name          TEXT NOT NULL,
  short         TEXT NOT NULL,
  primary_color TEXT NOT NULL DEFAULT '#F26A1B',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at    TIMESTAMPTZ,
  UNIQUE (tenant_id, slug)
);

CREATE TABLE project_members (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       TEXT NOT NULL CHECK (role IN ('owner', 'editor', 'reader')),
  can_control BOOLEAN NOT NULL DEFAULT false,
  PRIMARY KEY (project_id, user_id)
);

CREATE TABLE entities (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  slug       TEXT NOT NULL,
  name       TEXT NOT NULL,
  attrs      JSONB NOT NULL DEFAULT '{}',
  source     TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ,
  UNIQUE (project_id, slug)
);
CREATE INDEX entities_kind ON entities (project_id, kind) WHERE deleted_at IS NULL;

CREATE TABLE edges (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  from_id    TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
  rel        TEXT NOT NULL,
  to_id      TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
  attrs      JSONB NOT NULL DEFAULT '{}',
  source     TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ
);
CREATE INDEX edges_from ON edges (from_id, rel);
CREATE INDEX edges_to   ON edges (to_id, rel);

CREATE TABLE documents (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  doc_type   TEXT NOT NULL,          -- markdown, floorplan, component, dbschema, process, ...
  title      TEXT NOT NULL,
  content    JSONB NOT NULL,          -- extracted block JSON (latest)
  ydoc       BYTEA,                   -- compacted Yjs state
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ
);

-- Favourites: per project (shared, set by editors) or per user (private).
CREATE TABLE favorites (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  user_id     TEXT REFERENCES users(id) ON DELETE CASCADE,  -- NULL = project favourite
  position    INTEGER NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX favorites_unique ON favorites (project_id, document_id, COALESCE(user_id, ''));

-- Append-only event log: history, live updates, audit and index rebuilds.
CREATE TABLE events (
  id         BIGSERIAL PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  actor      TEXT NOT NULL,           -- user:<id>, agent:<id>, plugin:<id>, mcp:<client>
  kind       TEXT NOT NULL,
  subject    TEXT NOT NULL,
  payload    JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE favorites;
DROP TABLE events;
DROP TABLE documents;
DROP TABLE edges;
DROP TABLE entities;
DROP TABLE project_members;
DROP TABLE projects;
DROP TABLE users;
DROP TABLE organizations;
DROP TABLE tenants;
