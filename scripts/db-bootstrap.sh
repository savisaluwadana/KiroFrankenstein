#!/bin/sh
# Create/repair dev roles and databases in the running compose db service.
# Safe to re-run; use this when the volume predates docker/initdb.
set -eu
cd "$(dirname "$0")/.."
docker compose up -d --wait db
docker compose exec -T db sh /docker-entrypoint-initdb.d/01-bootstrap.sh
