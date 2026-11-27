-- A decision record carries the calls of discarded (stale) answers of the same
-- window as well as its own: llm_calls_charged can exceed 1 (task M7 PRIORITY 2).
-- The 0..1 CHECK of 00003 made the record insert fail after a stale answer,
-- so the window's commands were stored without its decision record.
-- Additive relaxation only; no row changes, triggers stay in place.

-- +goose Up
ALTER TABLE decision_records DROP CONSTRAINT decision_records_llm_calls_charged_check;
ALTER TABLE decision_records
  ADD CONSTRAINT decision_records_llm_calls_charged_check CHECK (llm_calls_charged BETWEEN 0 AND 32);

-- +goose Down
-- NOT VALID: rows written under the relaxed range stay (the table is append-only).
ALTER TABLE decision_records DROP CONSTRAINT decision_records_llm_calls_charged_check;
ALTER TABLE decision_records
  ADD CONSTRAINT decision_records_llm_calls_charged_check CHECK (llm_calls_charged BETWEEN 0 AND 1) NOT VALID;
