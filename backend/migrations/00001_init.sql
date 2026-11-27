-- Parallel initial schema (Req 28.1, 28.2, 28.5; design.md "Database Schema").
--
-- Roles (created by docker/initdb/01-bootstrap.sh, never by this file):
--   parallel_migrator  owns the database and every object here; runs migrations.
--   parallel_app       runtime role. It has NO DDL: no CREATE on any schema, no
--                      ownership. Its table grants are listed at the end and are
--                      the minimum the store needs. In particular it has no
--                      UPDATE, DELETE or TRUNCATE on input_log (INV-3); triggers
--                      below reject them for every role as well.
--
-- Mappings:
--   seed (uint64) is stored in bigint as its two's-complement bit pattern:
--     int64(seed) on write, uint64(v) on read. Equality and round-trip are exact.
--   scenario_json and payload are bytea so the canonical bytes (and therefore
--     scenario_hash and the input encoding) are stored verbatim, not re-ordered.
--   input_log.kind: 0 = command, 1 = disruption (sim.InputKind).

-- +goose Up
CREATE TABLE worlds (
  id             uuid        PRIMARY KEY,
  name           text        NOT NULL CHECK (char_length(name) <= 64),
  scenario_hash  bytea       NOT NULL CHECK (octet_length(scenario_hash) = 32),
  scenario_json  bytea       NOT NULL,
  seed           bigint      NOT NULL,
  engine_version text        NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE branches (
  id           uuid        PRIMARY KEY,
  world_id     uuid        NOT NULL REFERENCES worlds(id),
  parent_id    uuid        REFERENCES branches(id),
  fork_tick    integer     NOT NULL DEFAULT 0 CHECK (fork_tick >= 0),
  head_tick    integer     NOT NULL DEFAULT 0 CHECK (head_tick >= 0),
  label        text        NOT NULL CHECK (char_length(label) <= 64),
  manager_kind text        NOT NULL CHECK (manager_kind IN ('human', 'ai_rule_based', 'ai_llm')),
  created_at   timestamptz NOT NULL DEFAULT now(),
  -- the root has no parent and fork tick 0; a child has a parent
  CHECK (parent_id IS NOT NULL OR fork_tick = 0),
  CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX branches_world_idx ON branches (world_id);
CREATE INDEX branches_parent_idx ON branches (parent_id);

-- Append-only input log, one row per accepted input.
CREATE TABLE input_log (
  id              bigserial   PRIMARY KEY,
  branch_id       uuid        NOT NULL REFERENCES branches(id),
  seq             bigint      NOT NULL CHECK (seq >= 1),
  effective_tick  integer     NOT NULL CHECK (effective_tick >= 0),
  kind            smallint    NOT NULL CHECK (kind IN (0, 1)),
  payload         bytea       NOT NULL,
  provenance      text        NOT NULL CHECK (provenance IN
                    ('human', 'ai_rule_based', 'ai_llm', 'ai_rule_based_fallback', 'scenario', 'injected')),
  idempotency_key text        CHECK (idempotency_key IS NULL OR char_length(idempotency_key) BETWEEN 1 AND 128),
  rationale       text,
  model_name      text,
  server_ts       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (branch_id, seq)
);
CREATE UNIQUE INDEX input_log_idempotency_uq ON input_log (branch_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;
-- serves the ancestor-walk query: inputs of one branch up to a tick, in order
CREATE INDEX input_log_branch_tick_idx ON input_log (branch_id, effective_tick, seq);

-- Reject UPDATE, DELETE and TRUNCATE for every role (Req 28.5, INV-3).
-- +goose StatementBegin
CREATE FUNCTION input_log_immutable() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'input_log is append-only: % rejected', TG_OP
    USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER input_log_no_update BEFORE UPDATE ON input_log
  FOR EACH ROW EXECUTE FUNCTION input_log_immutable();
CREATE TRIGGER input_log_no_delete BEFORE DELETE ON input_log
  FOR EACH ROW EXECUTE FUNCTION input_log_immutable();
CREATE TRIGGER input_log_no_truncate BEFORE TRUNCATE ON input_log
  FOR EACH STATEMENT EXECUTE FUNCTION input_log_immutable();

-- Derived cache: trusted only when state_hash equals the replayed hash.
CREATE TABLE snapshots (
  branch_id  uuid    NOT NULL REFERENCES branches(id),
  tick       integer NOT NULL CHECK (tick >= 0),
  state_hash bytea   NOT NULL CHECK (octet_length(state_hash) = 32),
  blob       bytea   NOT NULL,
  PRIMARY KEY (branch_id, tick)
);

-- Derived projection cache: one row per computed tick.
CREATE TABLE tick_summaries (
  branch_id  uuid    NOT NULL REFERENCES branches(id),
  tick       integer NOT NULL CHECK (tick >= 0),
  state_hash bytea   NOT NULL CHECK (octet_length(state_hash) = 32),
  metrics    jsonb   NOT NULL,
  fcr        jsonb   NOT NULL,
  PRIMARY KEY (branch_id, tick)
);

-- Runtime role grants. No DDL, and nothing beyond what the store needs.
-- +goose StatementBegin
DO $grants$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'parallel_app') THEN
    REVOKE ALL ON SCHEMA public FROM PUBLIC;
    GRANT USAGE ON SCHEMA public TO parallel_app;
    GRANT SELECT, INSERT ON worlds TO parallel_app;
    GRANT SELECT, INSERT ON branches TO parallel_app;
    GRANT UPDATE (head_tick) ON branches TO parallel_app;
    GRANT SELECT, INSERT ON input_log TO parallel_app;          -- no UPDATE/DELETE/TRUNCATE
    GRANT USAGE ON SEQUENCE input_log_id_seq TO parallel_app;
    GRANT SELECT, INSERT, DELETE ON snapshots TO parallel_app;  -- cache: may be invalidated
    GRANT SELECT, INSERT, UPDATE, DELETE ON tick_summaries TO parallel_app;
  END IF;
END
$grants$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE tick_summaries;
DROP TABLE snapshots;
DROP TRIGGER input_log_no_truncate ON input_log;
DROP TRIGGER input_log_no_delete ON input_log;
DROP TRIGGER input_log_no_update ON input_log;
DROP FUNCTION input_log_immutable();
DROP TABLE input_log;
DROP TABLE branches;
DROP TABLE worlds;
