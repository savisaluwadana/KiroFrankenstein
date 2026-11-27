#!/bin/sh
# Create/repair dev roles and databases. Safe to re-run.
# Default: the compose db service (use this when the volume predates docker/initdb).
# CI / external server: set PGHOST (and PGPORT, PGPASSWORD, POSTGRES_USER) and the
# same bootstrap runs with the local psql client against that server.
set -eu
cd "$(dirname "$0")/.."
if [ -n "${PGHOST:-}" ]; then
  exec sh docker/initdb/01-bootstrap.sh
fi
docker compose up -d --wait db
docker compose exec -T db sh /docker-entrypoint-initdb.d/01-bootstrap.sh
