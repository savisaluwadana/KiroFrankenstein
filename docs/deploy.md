# Deployment and operations

Nothing in the repository deploys itself. The artifacts are `backend/Dockerfile`, `web/vercel.json`, `scripts/check-image-secrets.sh`, `scripts/smoke-prod.sh` and the untested templates in `docs/deploy-templates/`. The deployment procedure comes first; reference sections (variables with bounds, headers, probes, shutdown, SSE) follow under [Reference](#reference).

## Architecture

```
 Browser ──HTTPS──> Vercel (Next.js web app, static + SSR shell)
    │                  NEXT_PUBLIC_API_BASE_URL is inlined at build time
    │
    └──HTTPS (direct, CORS, SSE)──> TLS proxy / platform edge ──HTTP──> parallel-api container (:8080)
                                       (buffering off, idle timeout > 15 s)          │ runtime role parallel_app
                                                                                     │ (no DDL)  sslmode=require
                                                                                     v
                                                                        Managed PostgreSQL 16
                                                                          ^ owner role parallel_migrator
                                       release step: `parallel-api migrate up` ──────┘ (separate, never at app start)
                                       optional: LLM provider (HTTPS, server-side key only)
```

Vercel does not proxy the API. The browser calls the API origin directly, so the API's `WEB_ORIGIN` must equal the exact production web URL.

## Prerequisites

- A container host that keeps one long-lived process (SSE streams, in-process rate-limit counters): Fly.io, Render, Railway, or Cloud Run with minimum instances = 1 and request timeout above the stream length. Serverless functions are not suitable.
- Managed PostgreSQL 16 with TLS, **automated backups and point-in-time recovery enabled**. The rollback procedure for a bad migration is a restore, so this is a prerequisite, not an extra.
- A TLS-terminating proxy (normally the platform's) in front of the API, and a Vercel project (or any Node host) for `web/`.
- Docker locally to build the image. Never build or push with credentials in build arguments.

## Environment variables by deployment unit

**API runtime container** (secrets in the platform secret store, never in the image or repository):

| Variable | Required in production | Secure production value |
|---|---|---|
| `ENV` | yes | `production` |
| `PORT` | no | `8080` (image default) |
| `DATABASE_URL` | yes | Runtime role `parallel_app`, `sslmode=verify-full` (or `require`). Secret |
| `WEB_ORIGIN` | yes | Exact Vercel production URL, e.g. `https://parallel.example.com` (https, no path, no trailing slash) |
| `TRUSTED_PROXY_HOPS` | yes, unless `CLIENT_IP_HEADER` is set | Number of proxies in front; see below |
| `CLIENT_IP_HEADER` | alternative to hops | Empty or `X-Real-IP`; see below. Exclusive with `TRUSTED_PROXY_HOPS` > 0 |
| `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL` | no | Set all three or none. `LLM_API_KEY` is a secret. `LLM_BASE_URL` must be https |
| `SHUTDOWN_TIMEOUT_SECONDS`, `HTTP_REQUEST_TIMEOUT_SECONDS`, `RATE_LIMIT_*`, `MAX_CONCURRENT_SIMULATIONS`, `WORLD_CREATE_PER_HOUR_PER_IP`, `LLM_*` limits | no | Defaults in the table above and `.env.example` |

**Migration step** (one-off container, same image):

| Variable | Required | Notes |
|---|---|---|
| `MIGRATE_DATABASE_URL` | yes | Owner role `parallel_migrator`, `sslmode=require` or stronger. Secret. Never set on the running API |

The API does not read `MIGRATE_DATABASE_URL`; the migrate command does not read `DATABASE_URL`.

**Web (Vercel project, Production environment)**:

| Variable | Required | Notes |
|---|---|---|
| `NEXT_PUBLIC_API_BASE_URL` | yes | API origin plus `/api/v1`, e.g. `https://api.example.com/api/v1`. Set in the Vercel dashboard before the build |

`NEXT_PUBLIC_*` values are inlined into the browser bundle at build time and are public. Never put an LLM key, database URL or any secret in a `NEXT_PUBLIC_*` variable. Changing the value requires a rebuild, because the CSP `connect-src` is also computed from it at build time. Preview deployments get a different URL: add a Preview-scoped `NEXT_PUBLIC_API_BASE_URL` only if you run a separate preview API, and give that API its own `WEB_ORIGIN` (the API allows exactly one origin, so previews cannot share the production API).

## Container image

```
docker build -t parallel-api ./backend
scripts/check-image-secrets.sh parallel-api
```

The image is multi-stage: `golang:1.25-alpine` builds a static binary (`CGO_ENABLED=0`, `-trimpath -ldflags '-s -w'`), and `gcr.io/distroless/static-debian12:nonroot` runs it as `nonroot:nonroot` with the only env default `PORT=8080`. Migrations (`go:embed` of `backend/migrations/*.sql`) and the flagship scenario are inside the binary. `HEALTHCHECK` runs `parallel-api healthcheck`, which GETs `http://127.0.0.1:$PORT/healthz` (distroless has no curl). Default command: serve. Alternative command: `migrate up|down|status`.

`scripts/check-image-secrets.sh` fails if the image env defines `LLM_*` or database variables, if the image history or binary contains credential URLs, API-key shapes or provider/model identifiers, or if the image runs as root. Export your real `LLM_API_KEY` and `DATABASE_URL` in the calling shell to also check for those exact values.

## Managed PostgreSQL setup

Run once as the database administrator (the same statements as `docker/initdb/01-bootstrap.sh`, with placeholder passwords you must replace; generate long random values):

```sql
CREATE ROLE parallel_migrator LOGIN PASSWORD '<MIGRATOR_PASSWORD>';
CREATE ROLE parallel_app LOGIN PASSWORD '<APP_PASSWORD>' NOSUPERUSER NOCREATEDB NOCREATEROLE;
CREATE DATABASE parallel OWNER parallel_migrator;
REVOKE ALL ON DATABASE parallel FROM PUBLIC;
GRANT CONNECT ON DATABASE parallel TO parallel_app;
```

Some managed services restrict `CREATE ROLE` or database ownership; create the roles through the provider's console and apply the same ownership and grants. Table-level grants for `parallel_app` are issued by the migrations themselves (`SELECT, INSERT` on worlds, branches, input log, decision records and duels, `UPDATE (head_tick)` on branches, limited `DELETE` on the snapshot and summary caches, no `UPDATE`/`DELETE`/`TRUNCATE` on the input log, no DDL). Connection strings:

```
DATABASE_URL=postgres://parallel_app:<APP_PASSWORD>@<host>:5432/parallel?sslmode=verify-full
MIGRATE_DATABASE_URL=postgres://parallel_migrator:<MIGRATOR_PASSWORD>@<host>:5432/parallel?sslmode=verify-full
```

Check the runtime role cannot create tables (expect `permission denied for schema public`):

```
psql "$DATABASE_URL" -c 'CREATE TABLE ddl_probe (a int)'
```

## Migration procedure

Migrations run as a separate release step with the owner role, never at API start (the runtime role cannot run DDL).

```
docker run --rm -e MIGRATE_DATABASE_URL='postgres://parallel_migrator:...@host/parallel?sslmode=require' parallel-api migrate status
docker run --rm -e MIGRATE_DATABASE_URL=... parallel-api migrate up
```

Use the image tag you are about to deploy. Run `migrate up` before starting the new API version. Migrations are additive and backward compatible, so the previous API version keeps working against the new schema. `migrate down` reverts one migration and can drop tables that hold the append-only input log. Use it only on a restored copy or an empty database, never on production data; recover production by restoring a backup instead. Take a backup or confirm a recent PITR point before every `migrate up`.

## API container host

Generic checklist (the tested path is local Docker only; see the templates in `docs/deploy-templates/`, labelled untested):

1. Push the image to your registry yourself; deploy by immutable tag (a git SHA), not `latest`.
2. Inject the environment variables above from the platform secret store.
3. One instance, or accept per-instance rate limits and world-creation counters.
4. Liveness probe `GET /healthz`, readiness probe `GET /readyz`, period 10 to 15 s, timeout 3 s, failure threshold 3. `/readyz` checks the database, so do not use it for liveness.
5. Termination grace period of at least 30 s (default `SHUTDOWN_TIMEOUT_SECONDS` is 25).
6. Proxy: response buffering off for `/api/v1/branches/*/progress`, idle timeout above 15 s (60 s or more), HTTP/1.1 or HTTP/2 to the client.
7. Expose only the TLS proxy, not port 8080 directly.

Local example (runtime image against a database you control):

```
docker run --rm -p 8080:8080 \
  -e ENV=production -e WEB_ORIGIN=https://parallel.example.com -e TRUSTED_PROXY_HOPS=1 \
  -e DATABASE_URL='postgres://parallel_app:...@host/parallel?sslmode=verify-full' parallel-api
```

`TRUSTED_PROXY_HOPS` per platform: set it to the number of proxies that append to `X-Forwarded-For` before the API. Typical values are 1 for Fly.io and Render (one platform proxy), and 1 for Cloud Run (Google front end); put a CDN or load balancer you operate in front and it becomes 2. Verify rather than assume: send a request with a forged `X-Forwarded-For` and confirm per-IP limits do not follow the forged value. `0` is correct only when clients connect to the container directly.

Rate limits are per process (see Not covered). Secrets are never logged by the API; rotate `LLM_API_KEY` and database passwords in the secret store and restart. Rotating a database password means changing the role password and the secret, then redeploying.

## Web host (Vercel)

`web/vercel.json` selects the Next.js framework with `npm ci` and `npm run build`. Create a Vercel project with root directory `web`, set `NEXT_PUBLIC_API_BASE_URL` for Production, and deploy from the Vercel dashboard or Git integration. Security headers come from `web/next.config.ts`. There is no `web/Dockerfile`: building a standalone image would change `next.config.ts` output settings, so Vercel is the supported frontend host.

## Release order and rollback

Release: backup check, `migrate status`, `migrate up`, deploy the API image, run the smoke test, then deploy the web app (if the API origin changed, rebuild it).

Rollback: redeploy the previous image tag (and the previous Vercel deployment). Because migrations are additive and backward compatible, the older API runs on the newer schema. If a migration itself was wrong, stop the API, restore the database from backup or PITR to a point before the migration, and redeploy the previous tag. Recorded inputs are never edited or deleted to repair data.

## Production smoke test

```
scripts/smoke-prod.sh https://api.example.com https://parallel.example.com
```

It needs only `curl`. Extra curl flags go in `CURL_OPTS` (for a local self-signed CA: `CURL_OPTS="--cacert ca.crt"`). It creates **one small demo world** through `POST /api/v1/demo` (it never deletes worlds) and advances it to tick 4 with the `writeToken` from that response (the rehearsal found that the earlier version sent no token and would have got 401 in production). Checks: `/healthz`, `/readyz`, `/api/v1/health` (engine version), CORS preflight allows exactly the web origin and refuses `https://evil.example`, API security headers (HSTS over https), no `unsafe-eval`, `/api/v1/managers` and LLM state, demo creation, advance to the first decision window, state read, an SSE connection of 3 s that must deliver `event: tick`, and the web `/` returns 200 with a CSP that has no `unsafe-eval` and whose `connect-src` includes the API origin. Exit status is non-zero if any check fails, and every failure is listed.

Local rehearsal (ports other than 3000, 3100, 8000 and 8080): build the web app with `NEXT_PUBLIC_API_BASE_URL=http://localhost:18099/api/v1`, run `npx next start -p 13100`, run the container with `-p 18099:8080 -e WEB_ORIGIN=http://localhost:13100`, then run the script with those two origins. Over plain http the HSTS check is skipped.

## Local production rehearsal results (2026-11-28)

Everything below ran on one macOS machine with Docker. Nothing was deployed and no cloud CLI was used. Repeat it with `scripts/rehearse-prod.sh up | smoke | down` (local only; containers `rh-db`, `rh-api`, `rh-caddy`, network `rh-net`, scratch dir `/tmp/parallel-rehearsal`; `down` removes all of it and never touches other containers). Setup: Postgres 16 with `ssl=on` and a self-signed certificate on 127.0.0.1:55433 (`pg_stat_ssl` showed `TLSv1.3` for the runtime role); the `parallel-api` image from `docker build -t parallel-api ./backend`; Caddy 2 with a self-signed certificate in front (`https://api.localtest.me:18443` to the API with `flush_interval -1`, `https://parallel.localtest.me:18444` to `next start -p 13100`); API with `ENV=production`, `WEB_ORIGIN=https://parallel.localtest.me:18444`, `TRUSTED_PROXY_HOPS=1`, `sslmode=require`. The web app was built with `NEXT_PUBLIC_API_BASE_URL=https://api.localtest.me:18443/api/v1 npm run build`.

| # | Check | Result |
|---|---|---|
| 1 | `migrate up` as `parallel_migrator` over TLS (`sslmode=require`) from the built image | PASS, version 7 |
| 2 | Startup rejects `sslmode=disable`, missing `sslmode`, http `WEB_ORIGIN`, localhost `WEB_ORIGIN`, missing `WEB_ORIGIN`, missing `TRUSTED_PROXY_HOPS` and `CLIENT_IP_HEADER`, missing `DATABASE_URL`, partial LLM variables, http `LLM_BASE_URL` | PASS: exit 1, each message names the variable. A wrong password gives a generic "cannot connect to PostgreSQL" message; neither the password nor the key appeared in any output |
| 3 | `/healthz`, `/readyz` over HTTPS through the proxy | PASS |
| 4 | HSTS: absent on plain http without `X-Forwarded-Proto`, present with `X-Forwarded-Proto: https` and through Caddy | PASS |
| 5 | CORS preflight from the exact `WEB_ORIGIN` allowed (incl. `X-Parallel-Write-Token`), from `https://evil.example` denied | PASS |
| 6 | Security headers (nosniff, no-referrer, CSP without `unsafe-eval`, no-store) | PASS |
| 7 | Rate limit with `RATE_LIMIT_RPS=1 BURST=5` through the proxy, spoofed `X-Forwarded-For` prefix changed on every request | PASS: 5 x 200 then 429 (the spoofed prefix did not change the bucket). Probes are exempt (8 x 200) |
| 8 | Direct access to the container with hops=1 | Expected weakness confirmed: each distinct `X-Forwarded-For` value is a new bucket (10 x 200). Never publish the container port; only the proxy |
| 9 | SSE through Caddy (`curl -N --max-time 20`) | PASS: `event: tick` frames immediately, `: heartbeat` at +15 s |
| 10 | Web production build against the https API; CSP `connect-src 'self' https://api.localtest.me:18443`, HSTS, `X-Frame-Options: DENY` | PASS |
| 11 | `scripts/smoke-prod.sh` against the https API and web origins (20 checks) | PASS after fixing the script (see below) |
| 12 | Real Chromium (`web/playwright.rehearsal.config.ts`, `ignoreHTTPSErrors`): start the demo, Continue, Play against the https API, no console errors, no failed requests, no 4xx/5xx | PASS (write token in `sessionStorage`, cross-origin advance and SSE worked) |
| 13 | Restart persistence: demo world, advance to tick 24, fork at 12, advance the fork; `docker stop`; start | PASS: state hashes at ticks 0, 12, 24 of both branches byte-identical after restart (`cmp`); same write token advances (to 36); no token gives 401, wrong token 403 |
| 14 | Graceful stop | PASS: SIGTERM, exit code 0, 0.29 s idle, log line `shutting down timeout=25s`, no secrets in logs. The `/readyz` 503 during drain was not observable on an idle server (the listener closes at once); it is covered by `TestShutdownEndsStreams` and `TestReadiness` |
| 15 | Postgres stopped | PASS: `/readyz` 503 problem+json, `/healthz` 200, reads and writes answer 500 `internal` problem+json with a generic detail (no stack trace, no connection string in the body; the log lines name the database user, not the password). Note these are 500, not 503 |
| 16 | Postgres restarted, API not restarted | PASS: `/readyz` 200 within 1 s of the poll, reads and an advance (to tick 40) work (pgx pool reconnects) |
| 17 | `docker kill` (SIGKILL) during 6 concurrent advances to the horizon | PASS: 4 requests were cut off and their branches stayed at tick 0 (no partial write), 2 had finished (tick 48, the first disruption onset). After restart all 6 hashes at the head tick matched `parallel-sim verify` on the exported inputs (`MATCH`). The kill landed after the earlier single-world advance had already finished, so this is a window of about 30 ms, not a proof for every interleaving |
| 18 | Migration drill on a scratch database: `status` (empty), `up` to 7, `status`, `down` one step (7 to 6), `up` again, `up` again (no-op) | PASS |
| 19 | Runtime role `parallel_app`: `CREATE TABLE` denied, `DROP/TRUNCATE/UPDATE/DELETE` on `input_log` denied (psql over TLS); `migrate up` with the runtime role fails | PASS |

Findings and fixes:

- `scripts/smoke-prod.sh` sent the advance without `X-Parallel-Write-Token`, so against any production-mode API it would fail at the advance step. Fixed: it reads `writeToken` from the demo response. It also gained `CURL_OPTS`.
- The Postgres server in the rehearsal accepts non-TLS connections (default `pg_hba.conf`); `sslmode=require` is enforced on the client side by the API's startup check. On a managed database, also require TLS on the server side.
- The owner-role `DELETE FROM input_log` printed `DELETE 0` because the table was empty (no commands had been submitted), so the trigger was not exercised by this drill; the triggers are covered by `immutability_test.go` in `internal/store/postgres`.
- With the database down the API answers 500, not 503. Left as is (feature freeze). The readiness probe is the signal for load balancers.
- The existing Playwright journey spec cannot run in "external servers" mode against these servers: `e2e/helpers.ts` hard-codes `http://localhost:18080/api/v1`. The small `web/rehearsal/https.spec.ts` covers the https path instead.

`migrate down` is destructive: the first migration drops the input log. It is for scratch databases only. A rollback in production is a restore from backup or PITR.

### Verified locally and not verified

| Verified locally | NOT verified (needs the real platform) |
|---|---|
| Image builds, runs non-root, `scripts/check-image-secrets.sh parallel-api` passes | Vercel build and deploy of `web/`, Vercel environment variable handling, preview URLs |
| Production startup validation, TLS to Postgres, role separation, migrations | A real container host (Fly, Render, Railway, Cloud Run): health-check wiring, grace period, secret store, min instances, sleep behaviour |
| Reverse proxy behaviour with one hop: forwarded IP, HSTS from `X-Forwarded-Proto`, SSE with buffering off | The platform proxy's real hop count, idle timeout and buffering for SSE (`docs/deploy-templates/*` are still untested) |
| CORS, CSP and the write-token flow in a real browser over https with a self-signed CA | Real DNS, a publicly trusted certificate, HTTP/2 or HTTP/3 edge behaviour |
| Restart, SIGKILL and database outage recovery on a local container | A managed Postgres: provider TLS chain and `verify-full`, role creation limits, backups and PITR, failover |
| Smoke script and rehearsal Playwright spec | Behaviour under real load or several replicas (limits are per process) |

Managed Postgres and poolers. The store takes only transaction-scoped advisory locks (`pg_advisory_xact_lock(hashtext(branch id))` inside the append transaction, `store/postgres/inputs.go`); there is no `LISTEN/NOTIFY`, no session-level lock and no `SET`. Those are compatible with transaction pooling. However `pgxpool` uses pgx's default query mode, which prepares and caches named statements, and a transaction-mode pooler (PgBouncer, Neon's pooled endpoint, Supabase's pooler on port 6543) can break that. This was not tested. Use the provider's direct (non-pooled) connection string for `DATABASE_URL` and `MIGRATE_DATABASE_URL`. If you must use a pooler, try adding `default_query_exec_mode=exec` (or `simple_protocol`) to the URL and re-run the smoke test; treat that as unverified until you do. The migration step should always use the direct connection. Neon and Supabase need `sslmode=require` or stronger; both accept `verify-full` with their published CA.

### Deployment checklist (for a person, in order)

1. Provision managed PostgreSQL 16 with TLS, backups and PITR on. Note the direct (non-pooled) host.
2. Create the roles and database (see "Managed PostgreSQL setup") with long random passwords.
3. `docker build -t parallel-api ./backend`, run `scripts/check-image-secrets.sh parallel-api`, push the image under an immutable tag.
4. Run `migrate status`, then `migrate up` with `MIGRATE_DATABASE_URL` (owner role, `sslmode=require` or stronger). Confirm version 7.
5. Deploy the API container with `ENV=production`, `DATABASE_URL` (runtime role), a placeholder `WEB_ORIGIN` you will correct in step 7, and `TRUSTED_PROXY_HOPS` set to the real number (test with a forged `X-Forwarded-For`), or `CLIENT_IP_HEADER=X-Real-IP` on Railway. Set the grace period above 25 s, liveness `/healthz`, readiness `/readyz`, proxy buffering off and idle timeout of 60 s or more, and at least one instance that never sleeps.
6. Note the public API URL. In Vercel create the project with root directory `web`, set `NEXT_PUBLIC_API_BASE_URL=https://<api-host>/api/v1` for Production, deploy.
7. Set the API's `WEB_ORIGIN` to the exact Vercel production URL (no path, no trailing slash) and redeploy the API.
8. Run `scripts/smoke-prod.sh https://<api-host> https://<web-host>`. All checks must pass.
9. Optional LLM: set `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL` and a long random `LLM_ACCESS_CODE`; redeploy; run the smoke test again. Set a spending limit on the provider key first (the app limits calls and tokens, not currency).
10. Write down the previous image tag and the previous Vercel deployment for rollback.

Cost and availability warnings: free tiers often put the API to sleep or cap connections. A sleeping API drops open SSE streams and resets the per-process rate-limit and world-creation counters. Keep at least one instance running, and expect a cost for managed Postgres with PITR and for LLM usage.

Rollback: redeploy the previous API image tag and the previous Vercel deployment. Migrations are additive, so the older API runs on the newer schema. If a migration itself was wrong, stop the API, restore the database from backup or PITR to a point before it, and redeploy the previous tag. Do not use `migrate down` on production data.

## Reference

Production readiness notes for `parallel-api` and the web app. Results are synthetic; see the README disclaimer.

## Environment variables (API)

Out-of-range or malformed values stop start-up with a message that names the variable. No message ever contains `DATABASE_URL`, `LLM_API_KEY` or the LLM endpoint.

| Variable | Default | Bounds | Meaning |
|---|---|---|---|
| `PORT` | 8080 | 1..65535 | Listen port |
| `DATABASE_URL` | unset | | PostgreSQL runtime-role URL. Unset selects the in-memory store (state is lost on restart) |
| `MIGRATE_DATABASE_URL` | unset | | Owner-role URL, used only by `parallel-api migrate` |
| `WEB_ORIGIN` | `http://localhost:3100` | one `http(s)` origin | The single allowed CORS origin, exact match. `*`, other schemes, paths and trailing slashes are rejected. Credentials are never allowed |
| `ENV` (or `PARALLEL_ENV`) | unset | | `production` turns on strict validation and HSTS |
| `TRUSTED_PROXY_HOPS` | 0 | 0..5 | Trusted reverse proxies in front of the API (see Client IP) |
| `CLIENT_IP_HEADER` | empty | empty or `X-Real-IP` | Single edge-set client IP header (see Client IP); mutually exclusive with `TRUSTED_PROXY_HOPS` > 0 |
| `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST` | 50, 100 | > 0 | Per-IP token bucket |
| `HTTP_REQUEST_TIMEOUT_SECONDS` | 30 | 1..120 | Context deadline of every request except the progress stream |
| `SHUTDOWN_TIMEOUT_SECONDS` | 25 | 1..120 | Drain time on SIGTERM or SIGINT |
| `MAX_CONCURRENT_SIMULATIONS` | 8 | 1..64 | Expensive requests at once (advance, fork, diff, journal, ledger summary, duel create and advance) |
| `WORLD_CREATE_PER_HOUR_PER_IP` | 60 | 1..10000 | World creations (`/worlds`, `/demo`, `/duels`) per client IP per hour, then 429 |
| `MAX_WORLDS` | 5000 | 1..1000000 | World cap for the memory store only, then 503. PostgreSQL is not capped |
| `WRITE_TOKEN_FAILURES_PER_MINUTE` | 20 | 1..10000 | Invalid write tokens or LLM access codes per client IP per minute, then 429 |
| `LLM_ACCESS_CODE` | unset | | When set, starting an LLM-backed opponent needs the header `X-Parallel-LLM-Code` with this value (constant-time compare). A secret: set it in the secret store. Recommended for any public deployment that configures the LLM |
| `LLM_DUELS_PER_DAY_PER_IP` | 5 | 1..1000 | LLM-backed starts (duel, world or fork with `ai_llm`) per client IP per 24 hours, then 429. Per process |
| `LLM_*` | see `.env.example` | | Optional LLM opponent |

Fixed limits: request body 256 KiB, 32 branches per world, 4 progress streams per client IP, names 1 to 64 printable characters. Server timeouts: ReadHeaderTimeout 5 s, ReadTimeout 15 s, WriteTimeout request timeout + 10 s (the progress stream clears its own write deadline), IdleTimeout 60 s, MaxHeaderBytes 16 KiB.

## Production mode

With `ENV=production` the server refuses to start unless:

- `WEB_ORIGIN` is set explicitly, uses `https` and is not localhost or a loopback address.
- `DATABASE_URL` is set and has `sslmode=require`, `verify-ca` or `verify-full`. A missing `sslmode` (the driver would use `prefer`) and `disable` are rejected. `verify-full` is recommended.
- `CLIENT_IP_HEADER` or `TRUSTED_PROXY_HOPS` is set explicitly (`TRUSTED_PROXY_HOPS=0` when nothing is in front).
- The LLM configuration is complete when any of `LLM_API_KEY` or `LLM_MODEL` is set, and `LLM_BASE_URL` uses `https`.

In production mode, responses carry `Strict-Transport-Security` when the request arrived over TLS or with `X-Forwarded-Proto: https`.

## API response headers

Every API response: `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cache-Control: no-store` (the progress stream sends `no-cache, no-transform`), `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`. The API serves no static documents.

## Client IP and proxies

Per-IP limits (rate limit, world creation, 4 streams per IP) key on the peer address. `X-Forwarded-For` is ignored unless `TRUSTED_PROXY_HOPS` = N > 0. Then the Nth entry from the right is used: with one proxy that is the address the proxy saw as its peer. Entries further left are client-controlled and are never used. If the header has fewer than N entries or the entry is not an IP address, the peer address is used. Set N to the exact number of proxies you operate; a value that is too small keys limits on a proxy address, and one that is too large lets a client choose its own key.

`CLIENT_IP_HEADER=X-Real-IP` is the alternative for an edge that overwrites that header on every request (Railway; see `docs/deploy-railway-supabase-vercel.md`). All per-IP limits (request rate, world creation, progress streams, write-token failures, LLM duels per day) then use that header only: exactly one value that parses as an IPv4 or IPv6 address (IPv4-mapped IPv6 is normalised to IPv4). A missing, repeated, comma-separated or invalid value falls back to the peer address with a warning (at most one per minute, value never logged). `X-Forwarded-For` is ignored in this mode and the peer address is never replaced by it. The setting is exclusive with `TRUSTED_PROXY_HOPS` > 0 (startup error). The allowlist is closed (`httpapi.AllowedClientIPHeaders`); to support another edge, add its header there only after confirming that the edge replaces client-supplied copies, and extend `clientip_internal_test.go`. The trust boundary is that edge: the API must not be reachable around it.

## Probes

| Endpoint | Purpose | Behaviour |
|---|---|---|
| `GET /healthz` | Liveness | 200 `{"status":"ok"}`. No dependency check |
| `GET /readyz` | Readiness | 200 `{"status":"ready"}` when not shutting down, the database answers a ping within 2 s (result cached 1 s) and the schema is present (cached 30 s). Otherwise a generic 503 `unavailable` problem. The memory store is always ready |
| `GET /api/v1/health` | Used by the web client | Unchanged |

`/healthz` and `/readyz` sit at the server root, answer GET and HEAD, are exempt from CORS and rate limiting, and write no log line when they succeed. They are documented in `backend/api/openapi.yaml` under the `Operations` tag.

## Graceful shutdown

On SIGTERM or SIGINT the server (1) marks `/readyz` not ready at once, (2) ends open progress streams, (3) stops accepting connections and drains in-flight requests for up to `SHUTDOWN_TIMEOUT_SECONDS`, (4) closes remaining connections, which cancels their contexts and any LLM call waiting on them, and (5) closes the database pool. Set the platform's termination grace period above `SHUTDOWN_TIMEOUT_SECONDS`.

## Server-sent events behind proxies

The progress stream sends `Content-Type: text/event-stream`, `Cache-Control: no-cache, no-transform`, `X-Accel-Buffering: no` and a heartbeat comment every 15 s. The proxy or platform idle timeout must exceed 15 s (60 s or more is safe) and response buffering must be off for `/api/v1/branches/*/progress`. Polling is the documented fallback if a platform breaks SSE.

## Web app headers

`web/next.config.ts` sets, in production builds only, a Content-Security-Policy plus `X-Content-Type-Options`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, a minimal `Permissions-Policy` and HSTS. The CSP is pragmatic: `script-src` and `style-src` keep `'unsafe-inline'` because Next.js emits inline bootstrap scripts and styles and a nonce would force dynamic rendering of every page. `'unsafe-eval'` is not allowed. `connect-src` is `'self'` plus the origin of `NEXT_PUBLIC_API_BASE_URL`, which is read at build time, so build with the production API URL. `img-src` allows `data:` and `blob:` and `worker-src` allows `blob:` for PixiJS. `frame-ancestors 'none'`.

## Capability model and known limitation

There are no accounts. Three credentials exist (full description in `docs/security-model.md`):

- A world, branch or duel ID is a random UUIDv4 (122 random bits). It is a **read-only** capability: whoever holds it can read that world, so a URL is a read-only link.
- A **write token** (random 256 bits, returned once as `writeToken` by `POST /worlds`, `/demo` and `/duels`) is required in the `X-Parallel-Write-Token` header on every request that changes a world (advance, commands, decision-window end, fork, disruptions, duel advance, commands, window end and disruptions). A missing token gives 401 `write-token-required`, a wrong one 403 `write-token-invalid`, before any state changes. Only the SHA-256 hash is stored (`worlds.write_token_hash`, migration 00007). The token is never in a URL or a log.
- The optional **LLM access code** (`LLM_ACCESS_CODE`) gates starting an LLM-backed opponent.

Consequences:

- Nothing lists or enumerates worlds, branches or duels. Unknown and malformed IDs give the same 404 `not-found` problem.
- An ID from one world is never accepted as a different world's resource (the assumptions route answers 403, diffs answer 422 `incomparable-branches`, duel branches answer 409 `duel-branch`).
- IDs travel in URLs, so they appear in browser history and in any proxy access log. Anyone with the ID can read the world, but not change it.
- **Legacy worlds** (created before migration 00007, no stored hash) are read-only for every write in production and writable otherwise, so old development data keeps working locally.
- The web app keeps the token in the creating tab's `sessionStorage`. A fresh tab or browser that opens the URL sees the world read-only.
- CORS allows the `X-Parallel-Write-Token` and `X-Parallel-LLM-Code` request headers from `WEB_ORIGIN` only.
- Put real spending limits at the LLM provider. The app limits calls and tokens, not currency.

Tests: `TestSessionIsolation`, `TestTrustedProxy`, `TestSecurityHeaders`, `TestCORSStrict`, `TestNameValidation`, `TestReadiness`, `TestConcurrencyLimit`, `TestRateLimit*` in `backend/internal/httpapi`, and `TestLoadConfig*`, `TestProductionConfig`, `TestStartupLogsContainNoSecrets` in `backend/cmd/parallel-api`.

## Not covered

- The per-request deadline is a context deadline. Waits (store, advance lock, simulation queue, LLM call) honour it; a CPU-bound advance that is already computing finishes (it is bounded by the scenario horizon).
- The 5000-world cap counts worlds created through this process. A failed duel creation after its world was made releases its slot although the world remains.
- Rate-limit and creation counters are per process. Run one replica or accept per-replica limits.
