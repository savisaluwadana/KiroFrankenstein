# Submission checklist (Kiroween 2026, Frankenstein)

**Source of the requirements, and its limits.** `https://kiroween.devpost.com/rules` and `https://kiroween.devpost.com/` returned HTTP 403 to our fetch tool, so we did **not** read the 2026 official rules. Items below come from (a) `.kiro/steering/hackathon.md` (the repository's own summary), (b) web-search snippets of the 2026 pages, and (c) secondary summaries of the **2025** edition (`dev.to/brian_mk/decoding-kiroween-4nm`, `kiroween-2025.devpost.com`). The 2026 text may differ. Each row says where it came from. **Read the live rules page yourself before submitting.** We never submitted anything.

## Requirements and current status

| # | Requirement | Source | Status | Evidence or what is missing |
|---|---|---|---|---|
| 1 | New project built during the submission period (search snippet: October 30 to December 4, 2026; projects started before October 30 are not eligible) | 2026 snippet; `hackathon.md` ("must be new") | ✓ by dates, user to confirm | First commit 2026-11-10 (`git log --reverse`). Note the first four commits (2026-11-10) are scaffolding for a different, removed concept; Parallel was approved 2026-11-26. Confirm the rules treat that as compliant. |
| 2 | Public repository | `hackathon.md`; 2025 summary | ✓ per remote | Remote `https://github.com/savisaluwadana/KiroFrankenstein`; `gh run list` works against it. **Verify visibility is Public** in repository settings (we did not check). |
| 3 | Open-source license, visible | `hackathon.md`; 2025 summary says OSI-approved | ✓ | `LICENSE` (MIT, "Copyright (c) 2026 Savi Saluwadana"). Confirm GitHub shows the license in the repo sidebar. |
| 4 | `.kiro/` directory at the root, tracked | `hackathon.md` | ✓ | `git ls-files .kiro` lists 12 files (7 steering, 5 spec files). `.gitignore` says not to ignore it. |
| 5 | Built using Kiro (2025 text: vibe coding, agent hooks, steering docs, MCP or specs) | 2025 snippet | ✓ steering and specs; no hooks or MCP | `docs/hackathon/kiro-evidence.md`. |
| 6 | Working demo (or working software) | `hackathon.md` | ◐ local only | Runs locally (`docs/demo-script.md`). **No public deployment.** |
| 7 | Setup instructions | `hackathon.md` | ✓ draft; README is being rewritten by another worker | `README.md`, `docs/demo-script.md` "Before you record". Re-read once the rewrite lands. |
| 8 | Demo video under three minutes, publicly accessible | `hackathon.md`; 2025 summary ("Judges are not required to watch past the three-minute mark") | ✗ pending | Script ready: `docs/hackathon/demo-video-script.md` (2:55 plan). Video not recorded or uploaded. |
| 9 | Project description (what it does, how built, etc.) | Devpost form | ✓ draft | `docs/hackathon/devpost-submission.md`. |
| 10 | Category selected: Frankenstein | `hackathon.md` | ✓ draft | `docs/hackathon/frankenstein-justification.md`. Select it in the Devpost form. |
| 11 | Team info (a 2026 snippet says teams of up to three) | secondary snippet | ✗ pending | `{{TEAM_NAMES}}`. Confirm team size and eligibility (age, residence) in the rules. |
| 12 | Submit on Devpost before the deadline | `hackathon.md`; 2026 snippet gives December 4, 2026, 2:00 pm Pacific Time as the end of the submission period | ✗ pending | The user submits. Confirm the exact deadline and timezone on the rules page. |
| 13 | Log real Kiro sessions accurately | `hackathon.md` | ✓ with caveats | `docs/kiro-build-log.md`. Stale items: its checklist is unchecked for "first Kiro implementation commit", "working public demo", "submission write-up"; post-M7 work (`a2f6119` onward) has no log entry. Update before submitting. |

## Things the rules text may require that we could not confirm

- Whether a live, publicly reachable demo URL is required, or whether a runnable repository plus a video is enough. We assume the latter is acceptable only if the rules say so.
- Whether the video must be on a specific platform, and whether it must be free of third-party music or trademarks.
- Whether AI-generated code and pre-submission planning files need to be disclosed. The build log already states what preceded Kiro.
- Judging criteria. `hackathon.md` says "Judges weigh impact, technological implementation with Kiro, product design, innovation, and presentation equally"; verify.
- Eligibility of a project whose repository has commits from before the submission period (the 2026-11-10 commits are inside the stated period, but the "Initial commit" concept differed).

## Current project status to mention honestly

- ✓ Local CI mirror passes (`scripts/ci-local.sh`, per the build log).
- ✓ GitHub Actions run 46133835646 (commit `0d1a192`): conclusion success, all six jobs passed (`openapi lint`, `go (amd64)`, `go (arm64)`, `web`, `docker build`, `e2e`). Earlier run 46126448247 (`e666480`) failed in `e2e` only (a test race, fixed). Re-check `gh run list --branch main` before submitting.
- ✓ Release candidate: `v0.1.0-rc.1` (tag); notes in `docs/release-notes-rc1.md`.
- ✗ Live LLM verification pending (optional; `docs/live-llm-verification.md`).
- ✗ Not deployed (`docs/deploy.md`: "Nothing in the repository deploys itself").
- ✗ Open tasks: fork transition timing (1.5 s vs 600 ms, 8.6) and duel reveal (8.7) in `tasks.md`; M8 tasks 9.1 to 9.4 are done.

## Placeholders to fill

| Placeholder | Where used | Who supplies it |
|---|---|---|
| `{{DEMO_URL}}` | `devpost-submission.md` Links; Devpost "Try it out" | The user, after deploying (or leave blank and say "run locally") |
| `{{VIDEO_URL}}` | `devpost-submission.md` Links; Devpost video field | The user, after recording and uploading |
| `{{TEAM_NAMES}}` | `devpost-submission.md` Team | The user |
| Deployment platform, API origin, web origin | `docs/deploy.md` steps | The user |
| Live LLM provider, model and key (optional) | `docs/live-llm-verification.md` | The user (credentials only they can supply); then update the "pending" statements |
| Repository visibility confirmation | Row 2 above | The user |
| Deadline and timezone confirmation | Row 12 above | The user |

## Final manual steps for the human, in order

1. Read the live rules at https://kiroween.devpost.com/rules and fix any item in this file that differs. Confirm the deadline and your timezone.
2. Confirm the GitHub repository is public and the license shows. Re-run `gh run list --limit 5` and confirm the latest `main` run is green. Fix or note any failure.
3. Update stale status text: `docs/ci.md`, `README.md` (another worker is rewriting it), `docs/kiro-build-log.md` (add post-M7 sessions only if they were really Kiro sessions; tick the checklist honestly).
4. Optional: deploy (`docs/deploy.md`, `scripts/smoke-prod.sh`, `scripts/rehearse-prod.sh`). If you do, fill `{{DEMO_URL}}` and change "not deployed" statements everywhere they appear (`devpost-submission.md`, `demo-video-script.md`, this file).
5. Optional: run the live LLM verification (`parallel-sim llm-check`) with your own provider key. Only then change "pending" to a result, and record the result in `docs/live-llm-verification.md`.
6. Re-run `scripts/demo-numbers.sh` against a fresh API and compare with `docs/demo-script.md`. Rehearse the click path.
7. Record the video (under 3:00) following `demo-video-script.md`. Upload it (public) and open it in a private window to confirm access. Fill `{{VIDEO_URL}}`.
8. Choose the gallery images from `screenshots.md`. Re-check each against the narration numbers.
9. Create the Devpost project: paste from `devpost-submission.md`, choose the Frankenstein category, add the repository link, video link, demo link (if any), team members.
10. Read the final text once for claims that are not in the repository. Submit yourself, and confirm the submission shows as submitted, not draft.
