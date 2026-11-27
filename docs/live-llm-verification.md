# Live LLM provider verification

**Current status: Live provider verification: PENDING — no credentials were available during development.**

Every LLM code path (client, limits, parser, validator gate, fallback) is covered by stub-provider tests. No test has called a real provider. The `llm-check` command below is the cheap, safe way to change that. Nothing in this document is a measurement until the "not run" cells are filled from a real report.

## Prerequisites

1. A provider account and API key. The initial documented provider is OpenRouter (https://openrouter.ai).
2. A model id chosen from the provider catalogue (https://openrouter.ai/models). The project hardcodes none.
3. An untracked `.env` at the repository root (copy of `.env.example`) with `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL` set. `.env` is git-ignored; never commit it.

Secrets are read from the environment only. `llm-check` has no flag that carries a secret.

## Run the check

```
cd backend
set -a; source ../.env; set +a
go run ./cmd/parallel-sim llm-check                       # free: config check + GET /models only
go run ./cmd/parallel-sim llm-check --confirm-paid-call --out ../docs/live-llm-report.json
```

The report is not committed automatically. Review it (it holds no secret) and commit it only if you choose.

`.env.example` also holds `DATABASE_URL`; `llm-check` ignores it.

### What it does

| Step | Request | Cost | Status labels |
|---|---|---|---|
| 1 Config | none | free | `verified`, or `pending` and exit 4 |
| 2 Model availability | `GET {LLM_BASE_URL}/models` with the bearer key; looks for the configured model id | free | `verified` (listed), `failed` (not listed, or key rejected with 401/403), `inconclusive` (endpoint unsupported, 5xx, timeout, odd format) |
| 3 Structured proposal | one `POST {LLM_BASE_URL}/chat/completions` through the production `llm.Manager.Propose` | **at most 1 paid request** | `verified` when a parsed structured proposal came back, else `failed` with the fallback reason |
| 4 Fallback probe | none | none | always `pending`: a live provider cannot be forced to answer malformed output. Stub tests cover it. `--probe-fallback` is accepted and changes nothing. |

Step 3 builds a real Observation: the flagship scenario (embedded `black-friday.json`, or `--scenario FILE`), seed 2026 (`--seed N`), run without commands to the demo fork tick. The prompt, strict parse and request limits are the production ones. The proposed commands are then run through `sim.Validate` with the scenario's real command budget.

Hard limits: at most 2 provider requests per invocation, enforced by the HTTP transport (a third request fails locally); no loops, no duels, no ensembles. Step 3 is skipped without `--confirm-paid-call`, and also when step 2 shows the model id is not listed or the key is rejected. `LLM_MAX_CALLS_PER_WORLD` (minimum 1), `LLM_MAX_TOKENS_PER_CALL`, `LLM_MAX_PROMPT_BYTES`, `LLM_REQUEST_TIMEOUT_SECONDS` apply as in the server. Lower `LLM_MAX_TOKENS_PER_CALL` (for example 512) to cap the cost further.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | No failure. Without `--confirm-paid-call` this does **not** mean live verified (`liveVerified` is false) |
| 3 | Usage, invalid environment value, I/O error |
| 4 | `LIVE VERIFICATION PENDING: no runtime API key configured` (base URL, key or model unset). No request was made |
| 5 | A live request was made and the check failed (availability check failed, timeout, HTTP error, invalid JSON, schema violation) |

### Report fields

JSON to `--out` (default stdout); human-readable progress goes to stderr. Only non-secret fields: UTC timestamp, provider host name (no path, credentials or query), model id, variable names set or missing, per-step status, HTTP status, latency in ms, provider-reported prompt, completion and total tokens, the reserved estimate (`ceil(prompt bytes / 3) + max_tokens`) for comparison, commands proposed, accepted, rejected with validator codes, the model rationale (untrusted text, JSON-escaped), the fallback reason, and the number of provider calls.

`liveVerified` is true only when a real completion call returned a parsed structured proposal, even if the validator rejected some commands. Otherwise it is false and `reason` says why. If the provider sent no usage, `usage.reportedByProvider` is false and the token numbers are the reservation, not a measurement.

## Record results

Fill the tables below only from a real report. Do not estimate.

### Latency and tokens (copy into `docs/benchmarks.md`, section "Live provider run")

| Field | Value |
|---|---|
| Date (UTC) | not run |
| Provider host | not run |
| Model id | not run |
| Models listing | not run |
| Models listing latency (ms) | not run |
| Completion HTTP status | not run |
| Completion latency (ms) | not run |
| Prompt tokens (provider) | not run |
| Completion tokens (provider) | not run |
| Total tokens (provider) | not run |
| Reserved estimate | not run |
| Commands proposed / accepted / rejected | not run |
| Rejection codes | not run |
| `liveVerified` | not run |

### Checklist to paste with the results

- [ ] `llm-check --confirm-paid-call` exit code 0 and `liveVerified: true`
- [ ] Model listing status (`verified` or `inconclusive` with reason)
- [ ] Latency, token usage (provider-reported: yes/no), reserved estimate
- [ ] Commands proposed, accepted, rejected with codes
- [ ] Report file reviewed for secrets (should contain none)
- [ ] One live duel through the UI (below): LLM window shows `ai_llm` provenance, or the fallback reason
- [ ] Provider dashboard spend matches the order of magnitude of the reported tokens

## One live duel through the UI

After `llm-check` shows `liveVerified: true`:

1. Start the API with the same environment (`set -a; source ../.env; set +a`). To run in memory, `unset DATABASE_URL MIGRATE_DATABASE_URL` first. `cd backend && go run ./cmd/parallel-api`.
2. `cd web && npm run dev` in another terminal.
3. Load Black Friday Meltdown, fork at the demo fork tick and start a duel with the LLM AI opponent. `GET /api/v1/managers` must list `ai_llm` as available.
4. Advance the duel to the horizon. A duel makes at most one provider call per decision window (every 4 ticks, 30 windows over 120 ticks), so keep `LLM_MAX_CALLS_PER_WORLD` at or above the window count you accept to pay for; a lower value makes later windows fall back with `budget_exhausted`.
5. In the branch view, each LLM window shows its provenance (`ai_llm` or `ai_rule_based_fallback` with the reason) and the rationale labelled "AI rationale (not verified)".

## Budget safeguards

- Per world: `LLM_MAX_CALLS_PER_WORLD` (default 40), `LLM_MAX_TOTAL_TOKENS_PER_WORLD` (default 200000).
- Server-wide per UTC day: `LLM_GLOBAL_MAX_CALLS` (2000), `LLM_GLOBAL_MAX_TOKENS` (5000000). For a first live run set low values, for example `LLM_MAX_CALLS_PER_WORLD=35`, `LLM_GLOBAL_MAX_CALLS=40`, `LLM_GLOBAL_MAX_TOKENS=200000`.
- Every call first reserves the prompt estimate plus `max_tokens`; with no budget left no request is made and the window falls back to the rule-based AI.
- Set a spend limit on the provider key in the provider dashboard. That is the only limit that covers tokens the provider bills but does not report.
