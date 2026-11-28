-- Write token for a world: only the SHA-256 hash is stored, the token itself
-- is shown once at creation. NULL marks a world created before this
-- migration (read-only for mutations in production, writable in development).
-- Additive; no row changes.

-- +goose Up
ALTER TABLE worlds ADD COLUMN write_token_hash bytea NULL
  CHECK (write_token_hash IS NULL OR octet_length(write_token_hash) = 32);

-- +goose Down
ALTER TABLE worlds DROP COLUMN write_token_hash;
