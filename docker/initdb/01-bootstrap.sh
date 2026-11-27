#!/bin/sh
# Idempotent dev bootstrap: roles parallel_migrator (owner) and parallel_app
# (runtime, no DDL), databases parallel and parallel_test. Dev passwords only.
# Runs on first volume init and can be re-run any time (scripts/db-bootstrap.sh).
set -eu
SU="${POSTGRES_USER:-parallel}"
psql_su() { psql -v ON_ERROR_STOP=1 --username "$SU" --dbname postgres "$@"; }

psql_su <<'SQL'
SELECT 'CREATE ROLE parallel_migrator LOGIN PASSWORD ''migrator_dev_password'''
  WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'parallel_migrator') \gexec
SELECT 'CREATE ROLE parallel_app LOGIN PASSWORD ''app_dev_password'' NOSUPERUSER NOCREATEDB NOCREATEROLE'
  WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'parallel_app') \gexec
SQL

for db in parallel parallel_test; do
  psql_su -tAc "SELECT 1 FROM pg_database WHERE datname = '$db'" | grep -q 1 \
    || psql_su -c "CREATE DATABASE $db OWNER parallel_migrator"
  psql_su -c "ALTER DATABASE $db OWNER TO parallel_migrator" \
           -c "REVOKE ALL ON DATABASE $db FROM PUBLIC" \
           -c "GRANT CONNECT ON DATABASE $db TO parallel_app"
done
