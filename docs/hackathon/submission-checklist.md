# Submission checklist (Kiroween 2026, Frankenstein)

**Final pre-deployment state, audit of 2026-11-28.** Development is frozen. Nothing is deployed, no video is recorded, and nothing has been submitted to Devpost.

## Source of the requirements, and its limits

`https://kiroween.devpost.com/rules` and the overview page `https://kiroween.devpost.com/` returned HTTP 403 to automated fetches on 2026-11-28. **The official 2026 text could not be read.** What we know comes from:

- (a) the search-engine index snippet of `https://kiroween.devpost.com/` (paraphrased): only new projects are eligible; the project must be built during the Submission Period, October 30 to December 4, 2026; projects started before October 30 (including Kiroween 2025 projects) are not eligible;
- (b) `.kiro/steering/hackathon.md` (the repository's own summary);
- (c) secondary sources and the 2025 edition.

Every row below marked UNVERIFIED must be checked against the official page by the user. **Open the official rules in a browser and tick each item yourself.**

## Requirements and current status

| # | Requirement | Source | Status | Evidence or what is missing |
|---|---|---|---|---|
| 1 | New project built during the Submission Period (Oct 30 to Dec 4, 2026) | 2026 snippet; `hackathon.md` | ✓ by dates; UNVERIFIED against official text | First commit `8cb6215`, 2026-11-10 09:33 +0530; all commits fall inside the period. The first four commits (2026-11-10) are pre-implementation scaffolding for a removed concept (see `docs/kiro-build-log.md`). Implementation started 2026-11-26 (`ddc7c1d`). Confirm the rules accept this. |
| 2 | Public repository | `hackathon.md` | ✓ repository is PUBLIC | `https://github.com/savisaluwadana/KiroFrankenstein` |
| 3 | OSI-approved open-source license, visible | `hackathon.md`; UNVERIFIED for 2026 | ✓ | `LICENSE` (MIT) at the repository root. |
| 4 | Tracked `.kiro/` directory at the repository root | `hackathon.md`; UNVERIFIED for 2026 | ✓ | `.kiro/specs` and `.kiro/steering` are tracked. No hooks, no MCP configuration. |
| 5 | Built using Kiro | `hackathon.md` | ✓ steering and specs; no hooks or MCP | `docs/hackathon/kiro-evidence.md`. |
| 6 | Working, functional application URL | `hackathon.md`; whether a live URL is required, and until when, is UNKNOWN | ✗ PENDING: not deployed | Runs locally (`docs/demo-script.md`). Deployment target and runbook in "Deployment status" below. |
| 7 | Setup and testing instructions | `hackathon.md` | ✓ | `README.md`, `docs/demo-script.md`. |
| 8 | Public demo video under three minutes | `hackathon.md`; UNVERIFIED for 2026 (hosting rules unknown) | ✗ not recorded | Script ready: `docs/hackathon/demo-video-script.md` (2:55 plan). Record it with the deployed URL. |
| 9 | Devpost text | Devpost form | ✓ drafted, not submitted | `docs/hackathon/devpost-submission.md`. |
| 10 | Category: Frankenstein | `hackathon.md` | ✓ drafted, not submitted | `docs/hackathon/frankenstein-justification.md`. Select it in the Devpost form. |
| 11 | Team information | UNVERIFIED (secondary snippet says up to three) | ✗ pending | `{{TEAM_NAMES}}`. Confirm team size and eligibility in the rules. |
| 12 | Submit on Devpost before the deadline | Snippet: December 4, 2026 | ✗ not submitted | Time zone not verified (an earlier snippet said 2:00 pm Pacific; unconfirmed). Verify on the official page. |
| 13 | Log real Kiro sessions accurately | `hackathon.md` | ✓ with caveats | `docs/kiro-build-log.md`. Re-check its checklist and post-M7 entries before submitting. |

## Rules audit: what we could not confirm

Check these on the official rules page, in a browser:

- The exact deadline and its time zone.
- Whether the application URL must be live during judging, and until when.
- Testing-access requirements: whether judges need credentials or an access code (for us, the LLM access code `LLM_ACCESS_CODE`; the rule-based AI needs none).
- Team composition and eligibility (size, age, residence).
- Video hosting rules (platform, music, trademarks).
- Whether a third-party API (OpenRouter) needs disclosure, and whether AI-generated code and pre-submission planning need disclosure. The build log states what preceded Kiro.
- Judging criteria. `hackathon.md` says impact, technological implementation with Kiro, product design, innovation and presentation are weighted equally; verify.
- Eligibility given that the repository's first four commits were scaffolding for a different, removed concept.

## Release and CI status (verified)

- **Release candidate:** tag `v0.1.0-rc.1` points to commit `5ea46e5`. That commit equalled `origin/main` HEAD at the time of the audit, before any later docs-only commits. Notes: `docs/release-notes-rc1.md`.
- **GitHub Actions run 46136137307** for `5ea46e5`: success, all six jobs (openapi lint, go amd64, go arm64, web, docker build, e2e playwright). Honest note: attempt 1's e2e job was cancelled without logs after about 55 minutes. Attempt 2 passed. It was a re-run of the failed job with no code change.
- **Previous run 46133835646** (`0d1a192`): success on the first attempt.
- Earlier run 46126448247 (`e666480`) failed in e2e only (a test race, since fixed).
- Re-run `gh run list --branch main --limit 5` before submitting; later docs-only commits are not covered by the RC tag.

## Deployment status (nothing provisioned)

- **Target:** Vercel (web), Railway (API, Hobby plan), Supabase Postgres, OpenRouter (optional LLM). Runbook: `docs/deploy-railway-supabase-vercel.md`.
- **Provisioned:** nothing. No account, project, database, domain or key has been created.
- **Production acceptance suite:** ready (`npm run e2e:prod`). A dry run passed locally against Postgres: 7 passed before a restart and 1 passed after; the LLM test was skipped. It has **not** been run against a real deployment.
- **Live LLM verification:** PENDING. No credentials exist, and it needs the user's approval of a paid call. The LLM manager is tested only with stub providers (`docs/live-llm-verification.md`).
- **Known deployment risks:**
  - Client IP on Railway: `CLIENT_IP_HEADER=X-Real-IP` (Railway edge header; X-Forwarded-For ignored). Verify after deploy with runbook step 3.5.
  - Supabase dedicated schema is not usable without a code change (`CheckMigrated` checks `public.<table>`). Use `public` and disable the Supabase Data API.
  - LLM prompt size: with `LLM_MAX_PROMPT_BYTES=32768`, 12 of 31 decision windows in a flagship duel exceed the cap and fall back to the rule-based manager with reason `budget_exhausted`. Raising it to 262144 covers all windows at about 4x token cost. This is the user's decision.
  - Supabase Free pauses after one week of inactivity and has no backups.
- **Costs** (researched 2026-11-28; verify on the pricing pages): Railway Hobby $5/month including $5 usage; Supabase Free $0, or Pro from $25/month; Vercel Hobby $0; OpenRouter pay-per-token, where the credit limit on the key is the only hard monetary cap.

## Placeholders to fill

| Placeholder | Where used | Who supplies it |
|---|---|---|
| `{{DEMO_URL}}` | `devpost-submission.md` Links; Devpost "Try it out" | The user, after deploying |
| `{{VIDEO_URL}}` | `devpost-submission.md` Links; Devpost video field | The user, after recording and uploading |
| `{{TEAM_NAMES}}` | `devpost-submission.md` Team | The user |
| Platform account details, API and web origins | runbook steps | The user |
| LLM provider, model, key, access code (optional) | `docs/live-llm-verification.md` | The user |
| Deadline and time zone confirmation | Row 12 | The user |

After deployment, change every "not deployed" statement (`devpost-submission.md`, `demo-video-script.md`, this file). After a real live LLM check, change "pending" to the recorded result.

## Final manual steps for the human, in order

1. Re-read the official rules at https://kiroween.devpost.com/rules in a browser and tick every item above, especially the unknowns in "Rules audit". Fix any row that differs.
2. Approve the paid resources (Railway Hobby, optionally Supabase Pro, OpenRouter credit).
3. Follow `docs/deploy-railway-supabase-vercel.md`. Verify client-IP handling (step 3.5). `LLM_MAX_PROMPT_BYTES=262144` decided for gated testing.
4. Run the smoke test, then `npm run e2e:prod` with `PHASE=before`, redeploy the API, then `PHASE=after`.
5. Optional: create an OpenRouter key with a credit limit, set `LLM_ACCESS_CODE`, run `parallel-sim llm-check --confirm-paid-call`, and run one live duel. Record the result in `docs/live-llm-verification.md`.
6. Record the video (under 3:00) following `docs/hackathon/demo-video-script.md`, using the deployed URL. Upload it publicly and open it in a private window to confirm access.
7. Fill `{{DEMO_URL}}`, `{{VIDEO_URL}}` and `{{TEAM_NAMES}}`.
8. Read the final Devpost text once for claims not backed by the repository. Submit only after your approval, and confirm the submission shows as submitted, not draft.
