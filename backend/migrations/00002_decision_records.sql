-- Per-window AI decision records (task 6.3; Req 18.3, 19.6, 20.18, 33.8).
--
-- One row per (branch, Decision_Window) an AI manager handled, written even
-- when no Command was proposed. It is recorded metadata: replay never reads it
-- and no state hash covers it. Like input_log it is append-only for every
-- role: triggers reject UPDATE, DELETE and TRUNCATE, and parallel_app has only
-- SELECT and INSERT.
--
-- proposed and rejected are JSON documents encoded by app.EncodeProposed and
-- app.EncodeRejected.

-- +goose Up
CREATE TABLE decision_records (
  branch_id       uuid        NOT NULL REFERENCES branches(id),
  window_tick     integer     NOT NULL CHECK (window_tick >= 0),
  manager_kind    text        NOT NULL CHECK (manager_kind IN ('ai_rule_based', 'ai_llm')),
  provenance      text        NOT NULL CHECK (provenance IN ('ai_rule_based', 'ai_llm', 'ai_rule_based_fallback')),
  model           text        NOT NULL DEFAULT '',
  fallback_reason text        NOT NULL DEFAULT '',
  rationale       text        NOT NULL DEFAULT '',
  proposed        jsonb       NOT NULL,
  accepted_count  integer     NOT NULL CHECK (accepted_count >= 0),
  rejected        jsonb       NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (branch_id, window_tick)
);

-- +goose StatementBegin
CREATE FUNCTION decision_records_immutable() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'decision_records is append-only: % rejected', TG_OP
    USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER decision_records_no_update BEFORE UPDATE ON decision_records
  FOR EACH ROW EXECUTE FUNCTION decision_records_immutable();
CREATE TRIGGER decision_records_no_delete BEFORE DELETE ON decision_records
  FOR EACH ROW EXECUTE FUNCTION decision_records_immutable();
CREATE TRIGGER decision_records_no_truncate BEFORE TRUNCATE ON decision_records
  FOR EACH STATEMENT EXECUTE FUNCTION decision_records_immutable();

-- +goose StatementBegin
DO $grants$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'parallel_app') THEN
    GRANT SELECT, INSERT ON decision_records TO parallel_app;  -- no UPDATE/DELETE/TRUNCATE
  END IF;
END
$grants$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER decision_records_no_truncate ON decision_records;
DROP TRIGGER decision_records_no_delete ON decision_records;
DROP TRIGGER decision_records_no_update ON decision_records;
DROP FUNCTION decision_records_immutable();
DROP TABLE decision_records;
