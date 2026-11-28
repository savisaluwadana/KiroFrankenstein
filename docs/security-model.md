# Security model

Plain-language description of who can do what in a Parallel deployment. It describes the public demo: anonymous visitors, no accounts, no personal data, no payments, one PostgreSQL database and an optional server-side LLM key. Results are synthetic simulation output, not forecasts.

## Three credentials

| Credential | What it is | What it allows | Where it lives |
|---|---|---|---|
| World, branch or duel ID | Random UUIDv4 in the page URL | **Read** everything about that world: state, frames, ledger, metrics, export, diffs, assumptions, progress stream, duel score and decisions | The URL. Safe to share: a link is a read-only copy |
| Write token | Random 256-bit value (base64url), returned once as `writeToken` when a world, demo or duel is created | **Change** that world: advance, submit commands, end a decision window, fork, inject a surprise, advance a duel | The creating browser tab's `sessionStorage`. The server stores only its SHA-256 hash. Never in a URL, never logged |
| LLM access code | Optional shared secret set by the operator in `LLM_ACCESS_CODE` | **Start** an LLM-backed opponent (`ai_llm` duel, world or fork) when the operator has set one | Typed by the visitor into the dialog; kept in component state only |

The write token travels in the `X-Parallel-Write-Token` header. The LLM access code travels in `X-Parallel-LLM-Code`. The rule-based AI never needs a code and works with no API key.

## What an attacker with only a world ID can and cannot do

Can: read every number of that world and replay it; open the studio in read-only mode; compare its branches (`POST /diffs` is a read).

Cannot: advance it, record commands or disruptions, end a decision window, fork it, or advance its duel. Every such request answers 401 `write-token-required` (no header) or 403 `write-token-invalid` (wrong token) before any state changes. Unknown or malformed IDs answer the same 404 as before, and IDs cannot be listed.

Repeated wrong tokens or wrong LLM codes from one IP are counted. After `WRITE_TOKEN_FAILURES_PER_MINUTE` failures (default 20) that IP gets 429 until the minute ends. A missing token is not counted, because a shared link legitimately has none.

Cannot guess a token: it has 256 bits from `crypto/rand`, compared in constant time over SHA-256 digests.

Can still do: use up shared capacity by creating their own worlds (see the limits below), and read the world's data if they obtain its ID (IDs appear in browser history and proxy logs, so share only links you are happy to have read).

## Legacy worlds

Worlds created before the write-token migration (`00007_world_write_token.sql`) have no stored hash. In production (`ENV=production`) they are **read-only**: every write answers 403. Outside production they stay writable so old local development data keeps working.

## Threat model of the public demo

| Threat | Control |
|---|---|
| Visitor changes someone else's world | Write token required on every mutating route; route-table test fails if a new mutating route is unguarded |
| Brute-forcing tokens | 256-bit tokens, constant-time compare, per-IP failure limit with 429 |
| Visitor burns the LLM budget | Optional access code, per-IP daily cap on LLM-backed starts, per-world and global daily call and token caps, concurrency cap, prompt cap, provider timeout |
| Resource exhaustion | Body cap 256 KiB, 32 branches per world, horizon-bounded advance, concurrent-simulation slots, per-IP request rate, per-IP world creation cap, 4 progress streams per IP |
| Prompt injection through the LLM | The model gets only the structured observation; output is strict JSON through the same validator as human commands; rationale is shown as escaped text |
| Leaked secrets | Environment variables only; never in responses, logs or the web bundle |
| Tampering with history | Append-only input log (database trigger), snapshots trusted only if their hash matches a replay |

## Limits (all per process unless noted)

| Limit | Default | Setting |
|---|---|---|
| Requests per IP | 50/s, burst 100 | `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST` |
| World creations per IP per hour | 60 | `WORLD_CREATE_PER_HOUR_PER_IP` |
| Invalid write tokens or LLM codes per IP per minute | 20 | `WRITE_TOKEN_FAILURES_PER_MINUTE` |
| LLM-backed starts per IP per 24 h (fixed window) | 5 (1..1000) | `LLM_DUELS_PER_DAY_PER_IP` |
| LLM access code | none (open, subject to the limits) | `LLM_ACCESS_CODE` |
| LLM calls per world | 40 | `LLM_MAX_CALLS_PER_WORLD` |
| LLM calls and tokens per UTC day, whole server | 2000 calls, 5,000,000 tokens | `LLM_GLOBAL_MAX_CALLS`, `LLM_GLOBAL_MAX_TOKENS` |
| LLM provider requests in flight | 4 | `LLM_MAX_CONCURRENT_REQUESTS` |
| Concurrent simulations | 8 | `MAX_CONCURRENT_SIMULATIONS` |
| Branches per world, progress streams per IP, body size | 32, 4, 256 KiB | fixed |

The per-IP counters (requests, creations, failures, LLM starts) live in memory and reset on restart; with several replicas each keeps its own. The global daily LLM call and token usage is rebuilt from the decision records in PostgreSQL after a restart, so an exhausted day stays exhausted (`TestAIRestartGlobal*`). One gap remains: a provider call that was reserved but whose decision record was never written (a crash in between) is not counted after the restart.

## There is no hard spending cap

The application limits **calls and tokens**. It does not know provider prices and cannot enforce a currency amount. If the limits above are loose enough and the model is expensive, the bill can still be larger than you intend. Put the real cap at the provider.

### Provider-side limits

OpenRouter (the documented provider):

- Credit is prepaid. Load only the amount you accept to lose, and do not enable automatic top-ups for the demo key.
- Create a dedicated key for the demo and set a credit limit on it when you create it. OpenRouter's documentation recommends a credit limit on every key and says a key that reaches its limit is rejected until the limit is raised or, for a daily, weekly or monthly limit, resets. See <https://openrouter.ai/docs/api-keys> (keys are managed on <https://openrouter.ai/keys>). Check that page for the current options, because the dashboard wording changes.
- OpenRouter also offers alerts for key spend; see <https://openrouter.ai/docs/guides/features/notifications>.
- Revoke: delete or disable the key on the keys page, then clear `LLM_API_KEY` and restart the API. Rotate the key after the judging period or after any suspected exposure.

Other OpenAI-compatible providers: use a dedicated project or key, set a hard monthly budget or spend limit where the provider offers one, add a budget alert at a fraction of it, restrict the key to the one model you configured, and revoke it when the event ends.

## Why there is no login system

The demo is anonymous and holds no personal data. A login would add accounts, password or identity handling and recovery flows, all of which are larger risks than the data protected. A write token gives the one property that matters here: a link can be shared for reading without letting the recipient change the world.

Trade-offs, stated plainly:

- Closing the tab loses the write token (`sessionStorage`). The world stays readable by its URL but can no longer be changed from that browser. Start a new business to continue.
- Reloading the tab keeps it. Another tab or window does not have it, so a copied URL opens read-only there.
- Anyone with access to the creating browser tab can change the world.
- There is no recovery for a lost token, by design: only the hash is stored.

## Client IP for per-IP limits

All per-IP limits (request rate, world creation, progress streams, write-token failures, LLM duels per day) use one function, `Server.clientIP`. Modes: (1) default, the socket peer address, with `X-Real-IP` and `X-Forwarded-For` ignored; (2) `TRUSTED_PROXY_HOPS=N` for a proxy you operate, the Nth `X-Forwarded-For` entry from the right; (3) `CLIENT_IP_HEADER=X-Real-IP`, exactly one header value that parses as an IP (IPv4-mapped IPv6 normalised), otherwise the peer address; `X-Forwarded-For` is never read in this mode. Modes 2 and 3 are mutually exclusive. Mode 3 trusts the edge proxy (Railway) to overwrite the header on every forwarded request; Railway's docs list the header but staff statements, not the docs, describe the overwrite (see `docs/deploy-railway-supabase-vercel.md`). Anyone who can reach the API without going through the edge, for example over Railway's private network, can choose any header value and so any bucket; keep the API the only service in its project. Failure of the assumption lowers limiter quality but exposes no data: capabilities are the write token and the world ID, not the IP.

## Deployment recommendation

For any public deployment either:

1. Leave the LLM unconfigured (`LLM_*` unset). Everything works with the rule-based AI, and no provider cost is possible; or
2. Set `LLM_ACCESS_CODE` and conservative `LLM_GLOBAL_MAX_CALLS` and `LLM_GLOBAL_MAX_TOKENS`, use a provider key with a credit limit, and give the code only to people who should use the LLM opponent.

Also run with `ENV=production`, a PostgreSQL URL with TLS and an explicit `CLIENT_IP_HEADER` or `TRUSTED_PROXY_HOPS` (see `docs/deploy.md`).
