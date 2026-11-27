-- Index for the server-wide daily LLM usage sum (M7 security hardening).
--
-- Additive only: a plain index on the append-only decision_records table.
-- Creating an index does not fire the row triggers and changes no data, so
-- the append-only guarantees (UPDATE, DELETE and TRUNCATE rejected) are intact.
-- The service sums llm_calls_charged, prompt_tokens and completion_tokens of
-- rows with created_at >= the start of the current UTC day at startup.

-- +goose Up
CREATE INDEX decision_records_created_at_idx ON decision_records (created_at);

-- +goose Down
DROP INDEX decision_records_created_at_idx;
