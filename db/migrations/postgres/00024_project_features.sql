-- +goose Up
-- Optional project tabs (ADR-0032): decisions, graph, model, files.
-- Existing projects keep all of them; new projects start with none.
ALTER TABLE projects ADD COLUMN features TEXT NOT NULL DEFAULT '';
SELECT set_config('phoedox.tenant', '*', true);
UPDATE projects SET features = 'decisions,graph,model,files';

-- +goose Down
ALTER TABLE projects DROP COLUMN features;
