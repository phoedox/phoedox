-- +goose Up
-- AI triage of feedback (ADR-0077): why the AI chose a priority, the topic it
-- takes for the same issue (moderators decide whether to merge), spam.
ALTER TABLE feedback_topics ADD COLUMN ai_note TEXT NOT NULL DEFAULT '';
ALTER TABLE feedback_topics ADD COLUMN duplicate_of TEXT NOT NULL DEFAULT '';
ALTER TABLE feedback_topics ADD COLUMN spam BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE feedback_topics DROP COLUMN spam;
ALTER TABLE feedback_topics DROP COLUMN duplicate_of;
ALTER TABLE feedback_topics DROP COLUMN ai_note;
