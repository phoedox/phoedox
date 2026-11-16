-- +goose Up
-- Folder (category) names per further language (ADR-0028): a JSON object
-- {"Architecture/Backend": {"de": "Backend"}} – the translated name of the
-- last part of each folder path.
ALTER TABLE projects ADD COLUMN category_names TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE projects DROP COLUMN category_names;
