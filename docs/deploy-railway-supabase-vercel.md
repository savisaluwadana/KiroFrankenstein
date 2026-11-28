# Runbook: Railway (API) + Supabase (PostgreSQL) + Vercel (web)

Status: **written, not executed on the real platforms.** Research was done on 2026-11-28 from the official pages linked in each section. Everything marked **UNVERIFIED** has not been run on the platform. The dedicated-schema test and the acceptance suite dry run in this document ran locally. Nothing in this repository provisions anything. Read `docs/deploy.md` first for the variable reference; this file is the ordered procedure for one concrete stack.

Results are synthetic. Placeholders look like `<THIS>`. Never paste real secrets into the repository, chat or screenshots.

## Findings that change the plan (read first)

1. **Do not use a dedicated Postgres schema on Supabase.** It was tested locally and fails: migrations and the store contract tests pass in a schema `parallel` (search_path set per role and database), but `parallel-api` refuses to start with "the database schema is not migrated", because `postgres.CheckMigrated` looks up tables as `public.<name>` (`backend/internal/store/postgres/store.go:47`). Fixing that is a one-line code change, which is frozen. Use schema `public` and protect it as in step 1.
2. **Client IP on Railway: use `CLIENT_IP_HEADER=X-Real-IP`, not `TRUSTED_PROXY_HOPS`.** Railway's [Specs & Limits](https://docs.railway.com/networking/public-networking/specs-and-limits) page lists `X-Real-IP` as the header the edge sets with the client's remote IP. It does not state overwrite semantics. Railway staff say on Railway Station that the HTTP edge overwrites `X-Real-IP`, `X-Forwarded-For`, `X-Forwarded-Proto` and `X-Forwarded-Host` on every forwarded request, that the second `X-Forwarded-For` entry changes between requests (so counting hops is unreliable), and that the socket peer is Railway's proxy with no published stable range ([thread](https://station.railway.com/questions/security-clarification-x-real-ip-and-x-115c7e09), 2026-11-22; staff statements researched 2026-11-28, not a contract). With `CLIENT_IP_HEADER=X-Real-IP` `parallel-api` keys every per-IP limit on that one header and ignores `X-Forwarded-For` completely. A value that is missing, repeated, a comma list or not an IP falls back to the socket peer (a shared bucket, never a client-chosen one). **Trust boundary:** the Railway edge. Traffic over the private network (`*.railway.internal`) bypasses the edge and can carry any headers, so the API must not be reachable by untrusted callers there: keep it the only service in its Railway project, and do not add other services or private-network clients. `CLIENT_IP_HEADER` and `TRUSTED_PROXY_HOPS` > 0 are mutually exclusive (startup error).
3. **LLM prompts outgrow the default prompt cap.** Measured with the production prompt builder, no network: the flagship observation is 2.6 KB at hour 0 and grows to 220 KB (Rule-based-managed run, seed 2026) or 340 KB (no commands). With `LLM_MAX_PROMPT_BYTES=32768` (default), 12 of 31 windows of the measured run exceed the cap; those windows fall back to the Rule-based AI with reason `budget_exhausted`, mostly in the middle of the crisis (hours 64 to 112). See [LLM cost](#llm-cost-formula-and-measured-prompt-size). Raising the cap makes the LLM play those windows and raises the cost about 4 times. This is a decision for the owner, not a bug.
4. **Supabase exposes `public` tables through its Data API unless you turn that off.** Our tables are created by `parallel_migrator` in `public`. Locally simulated (roles `anon`, `authenticated`, `service_role` and default privileges granted by the admin role): tables created by `parallel_migrator` get **no** grants for those roles, so they are not reachable. Still disable the Data API (step 1.4) and run the check query (step 2.3).
5. **Railway Free and Trial plans are unsuitable.** Free services sleep after 5 to 10 minutes without outbound traffic; a Limited Trial restricts outbound ports ([Free plan](https://docs.railway.com/pricing/free-plan)). Use **Hobby** and make sure serverless/app sleeping is off.
6. **Supabase Free pauses after 1 week of inactivity and has no backups; PITR is a $100/month add-on on paid plans** ([pricing](https://supabase.com/pricing)). `docs/deploy.md` calls PITR a prerequisite. For a time-boxed judged demo, daily backups on Pro (or accepting no backups on Free, with the demo reproducible from the seed) is a deliberate deviation you must accept.

## Estimated monthly cost (checked 2026-11-28; verify on each pricing page before you commit)

| Provider | Plan and what is used | Cost | Source |
|---|---|---|---|
| Railway | Hobby: $5/month subscription that includes $5 of usage. Usage is billed at $10/GB RAM/month, $20/vCPU/month, $0.05/GB egress. A small Go API (about 0.1 GB RAM, low CPU) should stay inside the included $5. This estimate is mine, not measured | **$5/month**, rises only if usage exceeds $5 | [plans](https://docs.railway.com/pricing/plans) |
| Supabase | Free: $0, 500 MB, **pauses after 1 week of inactivity, no backups.** Pro: from $25/month, includes 8 GB and $10 compute credit (one Micro), daily backups kept 7 days, never paused. PITR $100/month per 7 days (add-on, not needed for the demo) | **$0 (Free) or $25 (Pro)** | [pricing](https://supabase.com/pricing) |
| Vercel | Hobby: free. The Hobby plan page lists its included usage; whether your use counts as non-commercial is Vercel's rule: **verify the fair-use terms** (not confirmed in this research) | **$0** | [Hobby](https://vercel.com/docs/plans/hobby) |
| OpenRouter | Prepaid credits; you pay the model's per-token price. The platform fee on credit purchases is **not verified: check openrouter.ai/pricing** | per formula below, capped by the key's credit limit | [limits](https://openrouter.ai/docs/api-reference/limits) |

Typical total without LLM: **$5/month (Supabase Free) or $30/month (Supabase Pro)**, plus LLM usage. A domain name, if you want one, is extra and not estimated here.

## Manual actions only you can do

| # | Action | Why it needs you |
|---|---|---|
| 1 | Create the GitHub, Railway, Supabase, Vercel and OpenRouter accounts; accept their terms | Accounts and legal acceptance |
| 2 | Add a payment card to Railway (Hobby) and, if chosen, Supabase Pro and OpenRouter credits | Billing |
| 3 | Choose the Supabase region and database password; generate and store all secrets | Secrets |
| 4 | Authorize the Railway and Vercel GitHub apps for this repository | Permission grant |
| 5 | Decide: Supabase Free vs Pro, `LLM_MAX_PROMPT_BYTES`, the OpenRouter model and credit limit | Spending and preference |
| 6 | Approve the one or two paid provider calls of `llm-check`, and the live LLM duel | Spending approval |
| 7 | Optional custom domains and DNS records | Domain ownership |
| 8 | Share `LLM_ACCESS_CODE` only with judges | Secret distribution |
| 9 | Teardown at the end (step 9) | Destructive actions on your accounts |

## 0. Prerequisites

- Local tools: `git`, `docker` (optional), Go 1.25+, Node 22+, `psql`, `curl`, `openssl`.
- Clean checkout of the release commit. Run the local quality gates you trust before deploying.
- Pick **one region** for both Railway and Supabase (for example both in the same continent) to keep database latency low. Railway region choice needs Hobby.
- Generate secrets locally (hex is URL-safe, so no percent-encoding is needed in connection strings):

```
openssl rand -hex 32   # MIGRATOR_PASSWORD
openssl rand -hex 32   # APP_PASSWORD
openssl rand -hex 24   # LLM_ACCESS_CODE (later)
```

## 1. Supabase project

1. Create a project (Dashboard, New project). Note the database password you set for the `postgres` role and the **project ref**.
2. Open **SQL Editor** and run, replacing the two placeholders:

```sql
CREATE ROLE parallel_migrator LOGIN PASSWORD '<MIGRATOR_PASSWORD>';
CREATE ROLE parallel_app LOGIN PASSWORD '<APP_PASSWORD>' NOSUPERUSER NOCREATEDB NOCREATEROLE;
-- The migrator owns our tables, so it needs to create objects in public.
GRANT USAGE, CREATE ON SCHEMA public TO parallel_migrator;
-- The runtime role only reads and writes through the grants the migrations issue. No CREATE.
GRANT USAGE ON SCHEMA public TO parallel_app;
```

   Migration 00001 also runs `REVOKE ALL ON SCHEMA public FROM PUBLIC` and `GRANT USAGE ON SCHEMA public TO parallel_app` as the migrator, which is not the owner of `public` on Supabase. In Postgres those statements then only warn. The statements above make the result explicit. Simulated locally with a non-owner migrator: migrations reached version 7, the runtime role had `USAGE` and no `CREATE`, and the API served a demo world. **UNVERIFIED on Supabase itself.**
3. Do not create the `parallel` database or change `search_path` (finding 1). Everything lives in the `postgres` database, schema `public`.
4. **Data API:** Dashboard, Integrations, Data API, turn **Enable Data API** off ([Securing your API](https://supabase.com/docs/guides/api/securing-your-api)). We never use it. Also keep **Automatically expose new tables** off if the option is shown.
5. **SSL:** Database Settings, enable SSL enforcement if it is not already on. Use `sslmode=require` in connection strings (the production check in `config.go` accepts `require`, `verify-ca`, `verify-full`). `verify-full` needs the Supabase CA file on disk, which the container does not have; `require` encrypts but does not verify the server certificate. Accept that or mount the CA yourself.
6. **Connection strings: use the shared pooler in session mode (port 5432).** From Connect, choose **Session pooler**; copy the host (`aws-<index>-<region>.pooler.supabase.com`, it cannot be composed from the region). The shared pooler is IPv4, which Railway needs (Railway does not support outbound IPv6, see [thread](https://station.railway.com/questions/do-hobby-plans-support-ipv6-a5a48bbf), and Supabase's direct connection is IPv6 unless you buy the IPv4 add-on). Custom roles connect as `<role>.<project-ref>` ([Connect docs](https://supabase.com/docs/guides/database/connecting-to-postgres)).

```
DATABASE_URL         = postgres://parallel_app.<PROJECT_REF>:<APP_PASSWORD>@<POOLER_HOST>:5432/postgres?sslmode=require&pool_max_conns=8
MIGRATE_DATABASE_URL = postgres://parallel_migrator.<PROJECT_REF>:<MIGRATOR_PASSWORD>@<POOLER_HOST>:5432/postgres?sslmode=require
```

   Why session mode: the store prepares and caches named statements (pgx default). Supabase documents that the shared pooler supports prepared statements in session mode and not in transaction mode (port 6543) ([KB](https://supabase.com/kb/troubleshooting/disabling-prepared-statements-qL8lEL)). The store uses only `pg_advisory_xact_lock` inside a transaction, no `LISTEN/NOTIFY`, no session `SET`; in session mode each client connection keeps one backend connection, so behaviour equals a direct connection. Transaction mode would also keep xact locks working but is not recommended here (prepared statements). `pool_max_conns=8` is a pgx pool parameter that keeps the API well below the pooler limits (client limits depend on compute size, see the pricing page); **UNVERIFIED against Supabase**.

## 2. Migrate from your laptop

1. Status, then up (expect `version: 7`):

```
cd backend
export MIGRATE_DATABASE_URL='<the migrator URL above>'
go run ./cmd/parallel-api migrate status
go run ./cmd/parallel-api migrate up        # expect: goose: successfully migrated database to version: 7
```

   The released Docker image works too: `docker run --rm -e MIGRATE_DATABASE_URL parallel-api migrate up`. Never run `migrate down` on this database.
2. The runtime role has no DDL (expect `permission denied for schema public`):

```
psql "$DATABASE_URL" -c 'CREATE TABLE ddl_probe (a int)'
psql "$DATABASE_URL" -c 'DELETE FROM input_log'      # expect permission denied
```

3. Nothing is exposed to the Supabase API roles (expect every row `f`):

```sql
SELECT c.relname,
       has_table_privilege('anon', c.oid, 'select') AS anon_select,
       has_table_privilege('authenticated', c.oid, 'select') AS auth_select,
       has_table_privilege('service_role', c.oid, 'select') AS svc_select
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relkind = 'r' ORDER BY 1;
```

   If any column is `t`, run `REVOKE ALL ON ALL TABLES IN SCHEMA public FROM anon, authenticated, service_role;` as `postgres` (it may only warn on tables you do not own; then connect as `parallel_migrator` and run it there) and keep the Data API off.

## 3. Railway service (API)

Sources: [Dockerfiles](https://docs.railway.com/builds/dockerfiles), [monorepo](https://docs.railway.com/deployments/monorepo), [healthchecks](https://docs.railway.com/guides/healthchecks), [specs and limits](https://docs.railway.com/networking/public-networking/specs-and-limits), [teardown](https://docs.railway.com/guides/deployment-teardown), [restart policy](https://docs.railway.com/deployments/restart-policy). Plan: **Hobby**.

1. New project, **Empty service** (so you can set the root directory before the first build). Settings: connect the GitHub repo; **Root Directory `/backend`**. Railway then finds `backend/Dockerfile` as `Dockerfile` and uses `backend/` as the build context, which is what the Dockerfile's `COPY go.mod go.sum ./` expects. (Alternative: leave the root and set `RAILWAY_DOCKERFILE_PATH=backend/Dockerfile`; the context would then be the repo root and the Dockerfile would fail, so prefer the root directory.)
2. Optional config as code: copy `docs/deploy-templates/railway.toml.template` to `backend/railway.toml` and set the config file path to `/backend/railway.toml` (the config file does not follow the root directory). Config as code is marked deprecated in the Railway docs; the same values can be set in the dashboard. The template is untested on the platform.
3. Variables (Railway injects `PORT`; the app already listens on `$PORT`, so do not set it):

| Variable | Value |
|---|---|
| `ENV` | `production` |
| `WEB_ORIGIN` | a placeholder `https://placeholder.example.com` now, the exact Vercel production URL in step 4 (https, no path, no trailing slash) |
| `DATABASE_URL` | the app role pooler URL from step 1.6 |
| `CLIENT_IP_HEADER` | `X-Real-IP` (verify in 3.5). **Do not set `TRUSTED_PROXY_HOPS`**; remove it if an earlier draft set `2` |
| `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST` | `50`, `100` (the defaults, stated so they are visible) |
| `MAX_CONCURRENT_SIMULATIONS` | `4` |
| `WORLD_CREATE_PER_HOUR_PER_IP` | `60` |
| `SHUTDOWN_TIMEOUT_SECONDS` | `25` |
| `RAILWAY_DEPLOYMENT_DRAINING_SECONDS` | `30` (Railway's default is 0: the old deployment gets SIGTERM and is killed at once; 30 gives the 25 s drain room) |
| `LLM_*` | **leave unset** for the first deploy |

   Do not set `MIGRATE_DATABASE_URL` here.
4. Settings: healthcheck path `/readyz`; restart policy On Failure (the default, 10 restarts); **serverless / app sleeping off**; generate a public domain (Networking). Healthchecks run only while a deployment goes live and are not continuous monitoring. Add an external uptime monitor if you want one.
5. **Verify the client-IP key once after deploy** (no setting changes; about 300 tiny requests to `/api/v1/health`, which is cheap; `/healthz` and `/readyz` are exempt from rate limiting). The default limit is 50 requests/s with a burst of 100, so the check must exceed that rate. The unit tests cover the logic; this step only confirms that the Railway edge behaves as its staff describe.
   a. From your laptop, send 300 requests, 32 in parallel, each with a different forged `X-Real-IP` and `X-Forwarded-For`:

```
seq 1 300 | xargs -P 32 -I{} curl -s -o /dev/null -w '%{http_code}\n' -H "X-Real-IP: 9.9.9.{}" -H "X-Forwarded-For: 8.8.8.{}" https://<API>/api/v1/health | sort | uniq -c
```

   Expected: some `429`s (the forged values did not give each request its own bucket). If every response is `200` the forged headers reach the app unchanged: stop, remove `CLIENT_IP_HEADER`, and leave `TRUSTED_PROXY_HOPS=0` (shared peer bucket) until the edge behaviour is understood. If you cannot reach 50 requests/s from your network, a few `200`s only is not a failure; raise the parallelism or rely on the tests.
   b. Wait 10 s, then from a second network (phone hotspot) one `curl https://<API>/api/v1/health` must return `200` while your laptop is limited. If it is limited too, the key is Railway's shared peer address (the header is absent or invalid): limits still hold but are shared by all visitors.
   c. Check the logs for `client IP header missing or invalid` (at most one line per minute, value never logged). Frequent lines mean the edge is not setting `X-Real-IP` as documented.
6. SSE and timeouts: the edge allows HTTP requests up to 15 minutes while data flows, closes idle HTTP/1.1 connections after 60 s between requests, and closes a request after 5 minutes with no data. The progress stream sends a heartbeat every 15 s, so it stays inside the limits; the browser reconnects with `Last-Event-ID` anyway. The community reports an undocumented 60 s cut for responses with no bytes; the heartbeat avoids it (**UNVERIFIED**, check with the smoke test's 3 s stream and a longer manual `curl -N`).
7. Redeploy behaviour: Railway keeps one deployment, starts the new one, switches when the healthcheck passes, then sends SIGTERM to the old one and SIGKILL after `RAILWAY_DEPLOYMENT_DRAINING_SECONDS`. `SHUTDOWN_TIMEOUT_SECONDS` (25) must be smaller than the draining time (30).
8. Smoke the API alone: `curl https://<API>/readyz` returns `{"status":"ready"}`.

## 4. Vercel project (web)

Sources: [monorepos](https://vercel.com/docs/monorepos), [Hobby](https://vercel.com/docs/plans/hobby).

1. Import the GitHub repo. **Root Directory `web`**. Framework Next.js (also set by `web/vercel.json`); install `npm ci`, build `npm run build`.
2. Environment variable, **Production**: `NEXT_PUBLIC_API_BASE_URL=https://<RAILWAY_DOMAIN>/api/v1`. It is inlined at build time and also drives the CSP `connect-src`, so changing it needs a redeploy. It is public: never put a secret in it.
3. Deploy. Note the exact **production URL** (the stable production domain, not a per-deployment URL).
4. Make sure the production deployment is public. Vercel Deployment Protection can require a Vercel login; check Settings, Deployment Protection, and open the URL in a private window (**UNVERIFIED default**).
5. Back in Railway set `WEB_ORIGIN` to that exact URL and redeploy the API.
6. **Preview deployments fail CORS by design:** the API allows exactly one origin. Do not test with preview URLs.

## 5. Production smoke

```
scripts/smoke-prod.sh https://<API> https://<WEB>
```

It creates one small demo world and must end with no `FAIL`. It also checks CORS for exactly the web origin, HSTS, the CSP, an SSE `tick` event and the web CSP `connect-src`.

## 6. Production acceptance (browser)

The suite is `web/prod/acceptance.spec.ts` with `web/playwright.prod.config.ts`. It starts nothing, makes no LLM call by default and creates about 6 small worlds per run (it never deletes them).

```
cd web && npm ci && npx playwright install chromium
export PROD_WEB_URL=https://<WEB> PROD_API_URL=https://<API>     # API origin, no path
PHASE=before npm run e2e:prod      # writes test-results/prod-phase.json (contains write tokens; keep it local)
# restart persistence: in Railway, Redeploy the API service (or Restart), wait until /readyz is ready
PHASE=after  npm run e2e:prod      # re-reads the recorded worlds and compares hashes
```

Without `PHASE` the suite runs once and the persistence test re-opens the worlds in a fresh browser context (no restart). The checklist coverage:

| Item | Test |
|---|---|
| Scenario creation: demo and custom setup | main flow, custom setup (starting cash 150,000 reaches the assumptions) |
| Playback to the first decision window, a decision with the cards | main flow (pauses at hour 4) |
| Rewind sends no POST, fork, comparison, What changed? | main flow (also on the 390x844 mobile project) |
| Rule-based Human vs AI duel to the horizon, scoreboard equals the API | duel (desktop) |
| Persistence | persistence test, and PHASE=after across a redeploy (hashes at tick 0, middle and head; stored write token still accepted; no token still 401) |
| Authorization: no token 401, wrong token 403, shared link read-only banner | authorization |
| CORS from `https://evil.example` denied, web origin allowed, security headers, CSP | limits and headers |
| Rate limiting | **not probed by the suite** (kept non-abusive). Use the manual check in step 3.5 (about 300 cheap requests to `/api/v1/health`) |
| LLM | only with `PROD_LLM_ACCESS_CODE` (step 7); at most 2 provider calls |

Dry run on 2026-11-28 against a local stack (API on 18085 with PostgreSQL, web production build on 13105, ENV=development): `PHASE=before` 7 passed (about 27 s), API process restarted, `PHASE=after` 1 passed (hashes and heads identical, no token still 401, stored token not refused). The mobile project ran the main flow and passed. The LLM test was skipped. Not run against any real deployment.

## 7. Enable OpenRouter (optional, spends money)

Sources: [limits](https://openrouter.ai/docs/api-reference/limits), [API reference](https://openrouter.ai/docs/api-reference/overview), [structured outputs](https://openrouter.ai/docs/guides/features/structured-outputs).

1. Create an OpenRouter account, buy a small amount of credits, and create an API key **with a credit limit** (the key creation form accepts an optional limit, and a reset interval; `GET https://openrouter.ai/api/v1/key` shows `limit_remaining`). Use $5 to $10. This is the only hard monetary cap; the app limits calls and tokens, not currency.
2. Choose a model from the OpenRouter catalogue yourself. It must follow instructions and answer in strict JSON (the app parses one JSON object and validates it; the catalogue can be filtered by structured-output support). Test it with `llm-check` before the deploy. This repository does not name a model.
3. Local live check with the same variables (needs your approval because it is a paid call):

```
cd backend
export LLM_BASE_URL=https://openrouter.ai/api/v1 LLM_API_KEY=<KEY> LLM_MODEL=<MODEL_ID> LLM_MAX_TOKENS_PER_CALL=512
go run ./cmd/parallel-sim llm-check            # no paid call
go run ./cmd/parallel-sim llm-check --confirm-paid-call --out llm-check.json    # at most 1 paid request
```

   Fill the "not run" cells of `docs/live-llm-verification.md` from the report.
4. Railway variables (all three of `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL` are required together; production start fails otherwise):

| Variable | Value |
|---|---|
| `LLM_BASE_URL` | `https://openrouter.ai/api/v1` |
| `LLM_API_KEY` | the key (secret) |
| `LLM_MODEL` | the model id you tested |
| `LLM_ACCESS_CODE` | the random code, shared only with judges |
| `LLM_MAX_CALLS_PER_WORLD` | `40` |
| `LLM_GLOBAL_MAX_CALLS` | `200` per UTC day (decided 2026-11-28 for gated testing with the 262144-byte prompt cap; about 6 full duels) |
| `LLM_GLOBAL_MAX_TOKENS` | `3000000` per UTC day (one full duel reserves about 0.5 million tokens at the 262144-byte cap: 1.4 MB of prompts / 3 plus 31 x 512) |
| `LLM_MAX_TOTAL_TOKENS_PER_WORLD` | `600000` (the default 200000 would stop a 262144-cap duel after a few windows) |
| `LLM_MAX_CONCURRENT_REQUESTS` | `2` |
| `LLM_DUELS_PER_DAY_PER_IP` | `3` |
| `LLM_MAX_TOKENS_PER_CALL` | `512` to halve the completion cost (default 1024) |
| `LLM_MAX_PROMPT_BYTES` | `262144` (decided 2026-11-28 for initial gated testing; the $5 OpenRouter key limit remains the hard monetary cap) |
| `LLM_HTTP_REFERER`, `LLM_APP_TITLE` | optional attribution headers (your site URL, an app name) |

5. Redeploy, then `curl https://<API>/api/v1/managers`: the LLM entry must be available, name the model, say `requiresAccessCode: true` and never show the key or base URL. Re-run the smoke test.
6. Acceptance with the LLM section (at most 2 provider calls):

```
PROD_LLM_ACCESS_CODE=<CODE> npm run e2e:prod
```

7. One live duel through the UI as a human: briefing, choose the LLM AI, enter the code. Each window shows `LLM AI (<model>)` or `Rule-based AI (LLM fallback: <reason>)`.

## 8. Rollback

- API: Railway, Deployments, pick the previous deployment, Rollback (images are kept 72 hours on Hobby, per [plans](https://docs.railway.com/pricing/plans)). Migrations are additive, so the older API runs on the newer schema.
- Web: Vercel, Deployments, promote the previous production deployment.
- Bad migration: stop the API, restore from a Supabase backup (Pro, daily), redeploy the previous tag. On Free there is no backup: recreate the project and re-run step 2; worlds are lost. Never use `migrate down` on production data.
- LLM misbehaving or too costly: remove `LLM_API_KEY` and redeploy (everything else keeps working), and revoke the key at OpenRouter.

## 9. Teardown after judging

1. OpenRouter: delete the API key; remove unused credits if you wish.
2. Railway: remove the service (Settings, Danger), then the project if empty; downgrade or cancel the Hobby subscription so it does not renew.
3. Supabase: pause the project, or delete it; cancel Pro if used. Download a backup first if you want to keep the data.
4. Vercel: delete the project or leave the free Hobby deployment; remove `NEXT_PUBLIC_API_BASE_URL` so the site cannot call a dead API.
5. Delete local `.env.production`, `test-results/prod-phase.json` and any `llm-check.json` containing provider details.

## LLM cost formula and measured prompt size

Per LLM call the app reserves `ceil(prompt_bytes / 3) + LLM_MAX_TOKENS_PER_CALL` tokens, then settles to the provider's reported usage (`backend/internal/policy/llm/config.go`). Real input is likely nearer `bytes / 4` for JSON; the reservation is deliberately conservative.

```
cost per duel = ( Tin x Pin + Tout x Pout ) / 1,000,000       Pin, Pout in $ per million tokens
Tin  = sum over LLM windows of (system bytes 1,395 + user prompt bytes) / (3 to 4)
Tout <= windows x LLM_MAX_TOKENS_PER_CALL
monthly cost = duels per month x cost per duel      (never above the OpenRouter key credit limit)
```

Measured with the production `SystemPrompt` and `UserPrompt` on the flagship scenario, seed 2026, command budget 5, no network, by a throwaway test that was deleted afterwards. The observation is driven by the Rule-based manager (a managed branch), 31 decision windows over 120 hours:

| Measure | Value |
|---|---|
| System prompt | 1,395 bytes |
| User prompt, hour 0 | 2,627 bytes (about 900 reserved prompt tokens with the system prompt) |
| User prompt, mean / max over the 31 windows | 43,815 / 220,656 bytes |
| Windows over the default 32,768-byte cap | 12 of 31 (fall back to the Rule-based AI, reason `budget_exhausted`, no cost) |
| LLM-eligible windows at the default cap | 19; their prompts total 226,412 bytes including the system prompt, so 56.6 to 75.5 thousand input tokens |
| Completion cap, 19 windows at 1,024 | at most 19.5 thousand output tokens (typical completions are shorter; not measured) |
| With `LLM_MAX_PROMPT_BYTES=262144`: 31 windows | 1,401,510 bytes, 350 to 467 thousand input tokens, at most 31.7 thousand output tokens |

A run with no commands reaches 340,492 bytes and exceeds even the largest allowed cap in the late windows. A duel starting at a later fork hour has fewer windows.

Illustrative prices, **hypothetical, not real quotes**, for a worst case at the upper token estimate:

| Hypothetical prices | Default cap (19 windows) | Cap 262144 (31 windows) |
|---|---|---|
| A: $0.50 per million in, $1.50 per million out | 75.5k x 0.50 + 19.5k x 1.50 = $0.038 + $0.029 = about **$0.07** per duel | 467k x 0.50 + 31.7k x 1.50 = about **$0.28** |
| B: $3 per million in, $15 per million out | 75.5k x 3 + 19.5k x 15 = $0.227 + $0.292 = about **$0.52** | 467k x 3 + 31.7k x 15 = about **$1.88** |

Look up the real prices of the model you choose on its OpenRouter page ("verify on the pricing page"). At price B, 400 daily calls bounded by 2.5 million tokens cost at most $7.50 if all tokens were input and $37.50 if all were output, so the **key credit limit ($5 to $10) is the binding cap**, not the app limits.

## Not verified

- Any behaviour on the real Railway, Supabase, Vercel or OpenRouter. All platform facts above are from the cited pages and community or staff posts of the dates shown; they can change.
- The Railway edge overwriting `X-Real-IP` (finding 2, `CLIENT_IP_HEADER=X-Real-IP`), the config-as-code field types in the template, SSE through the Railway edge for more than 15 s.
- The Supabase privilege model for `public` (simulated locally), the pooler client and pool-size limits for your compute size, `pool_max_conns` against Supavisor, and the Vercel production deployment protection default.
- Vercel Hobby fair-use terms for this project, OpenRouter credit purchase fees, and real model prices.
