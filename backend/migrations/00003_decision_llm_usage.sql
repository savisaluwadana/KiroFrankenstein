-- LLM usage per AI decision record (task 6.5; Req 20.14, 20.16).
--
-- Additive only: three columns on the append-only decision_records table, so
-- the per-World LLM call and token counters survive a restart (the service
-- rebuilds them by summing these columns over the World's Branches). Existing
-- rows take the defaults. ALTER TABLE ADD COLUMN does not fire the row
-- triggers, and the INSERT grant of parallel_app covers the new columns. The
-- UPDATE, DELETE and TRUNCATE triggers stay in place.
--
-- llm_calls_charged is 1 when a request was reserved against the World's
-- LLM_Call_Budget for that window (including a request that failed), 0 when
-- none was made (rule-based decisions, budget_exhausted fallbacks).

-- +goose Up
ALTER TABLE decision_records
  ADD COLUMN llm_calls_charged  smallint NOT NULL DEFAULT 0 CHECK (llm_calls_charged BETWEEN 0 AND 1),
  ADD COLUMN prompt_tokens      integer  NOT NULL DEFAULT 0 CHECK (prompt_tokens >= 0),
  ADD COLUMN completion_tokens  integer  NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0);

-- +goose Down
ALTER TABLE decision_records
  DROP COLUMN completion_tokens,
  DROP COLUMN prompt_tokens,
  DROP COLUMN llm_calls_charged;
