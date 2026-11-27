-- Duels (task 6.7; Req 21.1, 22.1).
--
-- One row per Duel: two sibling branches of one parent, forked at fork_tick,
-- one managed by the human and one by an AI. It is recorded metadata: replay
-- and state hashes never read it. Like input_log and decision_records it is
-- append-only for every role: triggers reject UPDATE, DELETE and TRUNCATE,
-- and parallel_app has only SELECT and INSERT.

-- +goose Up
CREATE TABLE duels (
  id                uuid        PRIMARY KEY,
  world_id          uuid        NOT NULL REFERENCES worlds(id),
  parent_branch_id  uuid        NOT NULL REFERENCES branches(id),
  human_branch_id   uuid        NOT NULL REFERENCES branches(id),
  ai_branch_id      uuid        NOT NULL REFERENCES branches(id),
  ai_kind           text        NOT NULL CHECK (ai_kind IN ('ai_rule_based', 'ai_llm')),
  fork_tick         integer     NOT NULL CHECK (fork_tick >= 0),
  comparison_policy text        NOT NULL CHECK (comparison_policy IN ('min_head_tick')),
  created_at        timestamptz NOT NULL DEFAULT now(),
  CHECK (human_branch_id <> ai_branch_id)
);
CREATE INDEX duels_world_idx ON duels (world_id);

-- +goose StatementBegin
CREATE FUNCTION duels_immutable() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'duels is append-only: % rejected', TG_OP
    USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER duels_no_update BEFORE UPDATE ON duels
  FOR EACH ROW EXECUTE FUNCTION duels_immutable();
CREATE TRIGGER duels_no_delete BEFORE DELETE ON duels
  FOR EACH ROW EXECUTE FUNCTION duels_immutable();
CREATE TRIGGER duels_no_truncate BEFORE TRUNCATE ON duels
  FOR EACH STATEMENT EXECUTE FUNCTION duels_immutable();

-- +goose StatementBegin
DO $grants$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'parallel_app') THEN
    GRANT SELECT, INSERT ON duels TO parallel_app;  -- no UPDATE/DELETE/TRUNCATE
  END IF;
END
$grants$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER duels_no_truncate ON duels;
DROP TRIGGER duels_no_delete ON duels;
DROP TRIGGER duels_no_update ON duels;
DROP FUNCTION duels_immutable();
DROP TABLE duels;
