---
inclusion: always
---

# Security steering

Threat model for the MVP: a public demo deployment with anonymous access, one PostgreSQL database and an optional server-side LLM key. No user accounts, no personal data, no payments.

## Rules

- **Secrets:** `DATABASE_URL`, `LLM_API_KEY`, `LLM_BASE_URL`, `LLM_MODEL` come from environment variables. Never commit them, never send them to the browser, never log them. Provide `.env.example` with placeholders only.
- **SQL:** pgx parameterized queries only. No string-built SQL. The application role has no DDL rights at runtime; migrations run with a separate role or step.
- **Input validation at the API boundary:** strict JSON decoding (unknown fields rejected), request bodies capped at 256 KiB, scenario assumptions validated against documented min/max bounds, names limited to 64 printable characters.
- **Resource limits:** per-world caps on branches (32), ticks (scenario horizon), commands per decision window (budget), SSE connections per client IP (4), and API rate limits per IP. Advancing a branch is bounded by the scenario horizon.
- **Identifiers:** world and branch IDs are random UUIDv4; possession of a world ID grants access to that world in the MVP. Document this as a known limitation.
- **Read-only links (stretch):** a share link is a separate random UUIDv4 that is a read-only capability. The server enforces it on every mutation endpoint (append inputs, fork, advance, end decision window, inject disruption, create/advance duel, create share) with 403, and responses through a share link omit the world ID. Never rely on the UI to hide mutation controls.
- **Ensembles:** CLI-only in the MVP, capped at 50 seeds per invocation, no network or LLM.
- **LLM boundary:**
  - The LLM receives only the structured observation and the command schema. User-supplied strings in the prompt are length-limited and labelled as data.
  - LLM output is parsed as strict JSON into commands and passed through the same validator as human commands. Rationale text is displayed as untrusted text (escaped, never rendered as HTML or markdown links).
  - Timeouts (20 s real time), at most one LLM call per decision window per branch, at most `LLM_MAX_CALLS_PER_WORLD` calls per world (default 40), server-wide per-UTC-day limits `LLM_GLOBAL_MAX_CALLS` (2000) and `LLM_GLOBAL_MAX_TOKENS` (5000000), at most `LLM_MAX_CONCURRENT_REQUESTS` (4) provider requests in flight, a prompt cap `LLM_MAX_PROMPT_BYTES` (32768), a conservative token reservation (estimated prompt tokens + `max_tokens`) before each call settled with provider-reported usage, strict proposal-schema validation, and a rule-based fallback that is recorded with provenance `ai_rule_based_fallback` and the fallback reason (`timeout`, `http_error`, `invalid_json`, `schema_violation`, `budget_exhausted`).
  - Optional `LLM_ACCESS_CODE`: when set, starting an `ai_llm` duel, world or fork needs header `X-Parallel-LLM-Code` (constant-time compare, 403 `llm-access-code-required`); the rule-based AI never needs it. `LLM_DUELS_PER_DAY_PER_IP` (default 5, 1..1000) caps LLM-backed starts per client IP per 24 h (429). Global daily usage is rebuilt from persisted decision records after a restart. The app cannot cap spending in currency: set a credit limit on the provider key.
  - No built-in default model identifier. OpenRouter is the initial documented provider; `.env.example` sets only its base URL and leaves the key and model empty.
- **Event log integrity:** a database trigger or constraint rejects UPDATE and DELETE on the input log table.
- **HTTP:** CORS restricted to the configured web origin; security headers (CSP without `unsafe-eval` in production, `X-Content-Type-Options`, `Referrer-Policy`); problem+json errors without stack traces.
- **Supply chain:** pinned dependency versions, `govulncheck` and `npm audit` in CI.

## Known limitations (state honestly in README)

- No accounts in the MVP. An ID is a read-only link; changing a world needs its write token (kept in the creating tab's sessionStorage, stored server-side only as a hash). There is no hard monetary spending cap in the app, only call and token limits; set a credit limit at the LLM provider (see `docs/security-model.md`).
- The LLM provider sees the observation data sent to it.

## Capability model and HTTP hardening (task 8.1)

- A world, branch or duel ID is a random UUIDv4 and is a **read-only** capability: holding it allows every read, never a mutation. Every mutating route (branch advance, commands, decision-window end, fork, disruptions; duel advance, commands, decision-window end, disruptions) needs the world's **write token** in `X-Parallel-Write-Token`: a random 256-bit value (crypto/rand, base64url) returned once as `writeToken` by `POST /worlds`, `/demo`, `/duels`, stored only as a SHA-256 hash (`worlds.write_token_hash`), compared in constant time, refused with 401 `write-token-required` or 403 `write-token-invalid` before any state change, never in a URL or a log, kept by the web app in `sessionStorage`. Invalid tokens are counted per IP (`WRITE_TOKEN_FAILURES_PER_MINUTE`, then 429). A legacy world with no hash is read-only in production and writable otherwise. `TestWriteRoutesNeedTheWriteToken` walks the router and fails for any unguarded mutating route; `POST /diffs` is a read. There is no user identity. There is no list endpoint, so IDs cannot be enumerated, and unknown or malformed IDs both return the same 404 `not-found` problem. A branch of another world is refused (403 on the assumptions route, `incomparable-branches` on diffs, `duel-branch` on duel branches). `TestSessionIsolation` pins this, including the route table.
- Client IP for per-IP limits is the peer address. `X-Forwarded-For` is trusted only when `TRUSTED_PROXY_HOPS` > 0, and then the Nth entry from the right is used. Alternatively `CLIENT_IP_HEADER=X-Real-IP` (closed allowlist, exclusive with hops > 0, for an edge such as Railway that overwrites it) takes the client IP only from that one single-IP header, falls back to the peer when it is missing or invalid, and ignores `X-Forwarded-For`.
- Expensive simulation requests share `MAX_CONCURRENT_SIMULATIONS` slots (503 with `Retry-After` after 5 s). World creation is capped per IP per hour and, for the memory store, in total.
- `ENV=production` refuses to start without an https `WEB_ORIGIN`, a `DATABASE_URL` with `sslmode=require`, `verify-ca` or `verify-full`, and an explicit `CLIENT_IP_HEADER` or `TRUSTED_PROXY_HOPS`. Details in `docs/deploy.md`.
