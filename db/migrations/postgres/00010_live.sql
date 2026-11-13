-- +goose Up
-- Live editing: the Yjs update log per document (y-websocket relay) and
-- the kind of a version, so live saves of one person can be merged.
CREATE TABLE doc_updates (
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  seq         BIGINT NOT NULL,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  data        BYTEA NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (document_id, seq)
);
ALTER TABLE document_versions ADD COLUMN kind TEXT NOT NULL DEFAULT 'edit';

-- +goose Down
ALTER TABLE document_versions DROP COLUMN kind;
DROP TABLE doc_updates;
