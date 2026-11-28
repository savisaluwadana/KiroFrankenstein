#!/bin/sh
# LOCAL production rehearsal. Not a deployment: nothing leaves this machine.
# Starts (all names prefixed rh-, own docker network, scratch dir under /tmp):
#   rh-db     Postgres 16 with ssl=on and a self-signed cert  127.0.0.1:55433
#   rh-api    parallel-api image, ENV=production, sslmode=require, TRUSTED_PROXY_HOPS=1
#   rh-caddy  Caddy 2 terminating TLS (self-signed) in front of API and web
#             https://api.localtest.me:18443  -> rh-api:8080  (flush_interval -1, SSE ok)
#             https://parallel.localtest.me:18444 -> next start on the host (:13100)
# Usage: scripts/rehearse-prod.sh up | smoke | down
# Needs docker, openssl, curl; the image from `docker build -t parallel-api ./backend`.
# The compose db (55432) and any container not named rh-* are never touched.
# *.localtest.me resolves to 127.0.0.1 through public DNS. Dev passwords only.
set -eu
cd "$(dirname "$0")/.."
D=/tmp/parallel-rehearsal
NET=rh-net
API_URL=https://api.localtest.me:18443
WEB_URL=https://parallel.localtest.me:18444
DBPASS_APP=app_dev_password
DBPASS_MIG=migrator_dev_password
CA="--cacert $D/certs/tls.crt"

api_env() { # extra -e flags are passed by the caller
  echo "-e ENV=production -e WEB_ORIGIN=$WEB_URL -e TRUSTED_PROXY_HOPS=1 \
    -e DATABASE_URL=postgres://parallel_app:$DBPASS_APP@rh-db:5432/parallel?sslmode=require"
}

down() {
  docker rm -f rh-caddy rh-api rh-db rh-migrate >/dev/null 2>&1 || true
  docker network rm $NET >/dev/null 2>&1 || true
  rm -rf "$D"
}

start_api() { # shellcheck disable=SC2120
  docker rm -f rh-api >/dev/null 2>&1 || true
  # shellcheck disable=SC2046
  docker run -d --name rh-api --network $NET -p 127.0.0.1:18098:8080 $(api_env) parallel-api >/dev/null
}

up() {
  down
  mkdir -p "$D/certs"
  openssl req -x509 -newkey rsa:2048 -nodes -days 2 -keyout "$D/certs/tls.key" -out "$D/certs/tls.crt" \
    -subj "/CN=rehearsal" -addext "subjectAltName=DNS:api.localtest.me,DNS:parallel.localtest.me,DNS:rh-db" 2>/dev/null
  chmod 644 "$D/certs/tls.key"
  docker network create $NET >/dev/null
  docker run -d --name rh-db --network $NET -p 127.0.0.1:55433:5432 \
    -e POSTGRES_USER=parallel -e POSTGRES_PASSWORD=parallel_dev_password -e POSTGRES_DB=parallel \
    -v "$PWD/docker/initdb:/docker-entrypoint-initdb.d:ro" -v "$D/certs:/certs:ro" postgres:16 \
    bash -c 'mkdir -p /tmp/ssl && cp /certs/tls.* /tmp/ssl/ && chown postgres:postgres /tmp/ssl/* && chmod 600 /tmp/ssl/tls.key &&
      exec docker-entrypoint.sh postgres -c ssl=on -c ssl_cert_file=/tmp/ssl/tls.crt -c ssl_key_file=/tmp/ssl/tls.key' >/dev/null
  i=0
  until docker exec rh-db sh -c 'pg_isready -h 127.0.0.1 -U parallel -d parallel' >/dev/null 2>&1 \
        && [ "$(docker exec rh-db psql -U parallel -d parallel -tAc "select count(*) from pg_roles where rolname='parallel_app'" 2>/dev/null)" = 1 ]; do
    i=$((i+1)); [ $i -lt 60 ] || { echo "db did not become ready" >&2; exit 1; }; sleep 1
  done
  docker run --rm --name rh-migrate --network $NET \
    -e "MIGRATE_DATABASE_URL=postgres://parallel_migrator:$DBPASS_MIG@rh-db:5432/parallel?sslmode=require" \
    parallel-api migrate up
  start_api
  cat > "$D/Caddyfile" <<EOF
{
	auto_https off
}
https://api.localtest.me {
	tls /certs/tls.crt /certs/tls.key
	reverse_proxy rh-api:8080 {
		flush_interval -1
	}
}
https://parallel.localtest.me {
	tls /certs/tls.crt /certs/tls.key
	reverse_proxy host.docker.internal:13100
}
EOF
  docker run -d --name rh-caddy --network $NET -p 127.0.0.1:18443:443 -p 127.0.0.1:18444:443 \
    -v "$D/Caddyfile:/etc/caddy/Caddyfile:ro" -v "$D/certs:/certs:ro" \
    --add-host host.docker.internal:host-gateway caddy:2 >/dev/null
  i=0
  until curl -s $CA -o /dev/null "$API_URL/readyz"; do
    i=$((i+1)); [ $i -lt 30 ] || { echo "api not reachable through caddy" >&2; exit 1; }; sleep 1
  done
  echo "up: API $API_URL  web proxy $WEB_URL (start web: see docs/deploy.md rehearsal section)"
}

case "${1:-}" in
  up) up ;;
  smoke) CURL_OPTS="$CA" scripts/smoke-prod.sh "$API_URL" "$WEB_URL" ;;
  down) down ;;
  *) echo "usage: $0 up|smoke|down" >&2; exit 2 ;;
esac
