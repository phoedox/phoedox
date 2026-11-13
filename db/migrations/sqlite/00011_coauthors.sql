-- +goose Up
-- People who edited a live version together with its author (user ids,
-- comma separated).
ALTER TABLE document_versions ADD COLUMN coauthors TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE document_versions DROP COLUMN coauthors;
