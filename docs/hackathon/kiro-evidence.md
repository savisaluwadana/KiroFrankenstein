# Kiro usage evidence

Only what the repository proves. Counts were computed with `grep`/`git` on the day this file was written; commands are given so judges can re-run them.

## Provenance note (read exactly)

`docs/kiro-build-log.md` says: "The initial steering, requirements, design, and tasks were prepared as repository scaffolding outside Kiro. Do not represent them as generated or executed by Kiro. Start logging only after using Kiro itself."

What the git history shows (`git log --reverse --format='%h %ad %s' --date=short`):

| Commit | Date | Subject |
|---|---|---|
| `8cb6215` | 2026-11-10 | Initial commit |
| `2a282fa` | 2026-11-10 | chore: add MIT license |
| `5c456a6` | 2026-11-10 | docs: initialize Kiro steering and incident-stitcher MVP specification |
| `9abdaf5` | 2026-11-10 | docs: remove unapproved concept and keep neutral Kiro hackathon setup |
| `ddc7c1d` | 2026-11-26 | feat: M0 repository scaffolding |

So the first four commits (2026-11-10) are scaffolding for a different, later-removed concept ("incident-stitcher"). They are **not** Kiro-generated evidence. The Parallel concept was approved on 2026-11-26, and the build log's first Kiro session is dated that day.

Credit consumption: **not measured.** The build log says so for M6 and M7 ("Kiro credit use ... was not measured"). We make no claim about credits used.

## `.kiro/` contents

```
.kiro/steering/   hackathon.md  product.md  security.md  simulation-invariants.md  structure.md  tech.md  ui-design-system.md
.kiro/specs/parallel-simulation-mvp/   .config.kiro  requirements.md  design.md  tasks.md  tasks.meta.json
```

No other directories exist. **There are no hooks (`.kiro/hooks`) and no MCP configuration (`.kiro/settings/mcp.json`) in the repository, and we do not claim to have used either.** The build log's own checklist leaves "Hook / custom agent / MCP usage documented if actually used" unchecked. (`find .kiro -name '*hook*' -o -name 'mcp*'` returns nothing.)

### Steering files (purpose)

| File | Purpose | Inclusion |
|---|---|---|
| `product.md` | Product, users, demo, principles, vocabulary | always |
| `tech.md` | Stack, simplifications, determinism rules, quality gates | always |
| `structure.md` | Repository layout, module boundaries, conventions | always |
| `simulation-invariants.md` | INV-1 to INV-19 the tests must cover | always |
| `security.md` | Threat model, LLM boundary, capability model | always |
| `hackathon.md` | Hackathon constraints and credit budget | always |
| `ui-design-system.md` | Visual language and authoritative-state UI rules (partly superseded by `docs/ux-redesign.md`, as the file says) | fileMatch `web/**` |

### Spec counts

| Artifact | Count | How computed |
|---|---|---|
| Requirements | 43 (37 MVP, 1 polish, 5 stretch per the build log) | `grep -c '^### Requirement' requirements.md` |
| Numbered acceptance criteria | at least 326 | `grep -cE '^[0-9]+\. (THE\|WHEN\|WHILE\|IF\|WHERE)' requirements.md` (lines starting with "FOR ALL" are not counted, so this is a lower bound) |
| Design correctness properties | 41 | `grep -cE '^#{2,4} Property' design.md` |
| Task checkboxes | 155 | `grep -cE '^\s*- \[' tasks.md` |
| Checked | 130 (7 top-level milestones and 123 sub-tasks) | `grep -cE '^\s*- \[x\]'` |
| Unchecked | 25 (11 are optional, marked `[ ]*`) | `grep -cE '^\s*- \[ \]'` |
| Reconciliation notes | 59 lines containing "Reconciliation" | `grep -c Reconciliation tasks.md` |

Still open in `tasks.md` (so the submission does not overstate completion): 8.6 fork split transition (delivered at 1.5 s against a 600 ms bound, marked as an accepted deviation in the notes), 8.7 duel result reveal, 9.1 to 9.4 (the M8 documentation tasks) and the stretch items 10.x.

## Spec-driven workflow actually followed

From `docs/kiro-build-log.md` (dated sessions, surface as logged):

| Date | Session | Surface logged |
|---|---|---|
| 2026-11-26 | Concept approved; steering and requirements | Kiro CLI (spec workflow, requirements-first) |
| 2026-11-26 | Requirements revised from user feedback | Kiro CLI |
| 2026-11-26 | Second requirements revision (final constraints) | Kiro CLI |
| 2026-11-26 | Design document generated | Kiro CLI |
| 2026-11-26 | Third requirements revision (resolutions) | Kiro CLI |
| 2026-11-26 | Design revised to match revision 4 | Kiro CLI |
| 2026-11-26 | Implementation plan (`tasks.md`) generated | Kiro CLI |
| 2026-11-26 | M0 + M1 implementation (vertical slice) | Kiro IDE agent orchestrating spec-task-execution subagents |
| 2026-11-27 | M2 + M3 implementation | same |
| 2026-11-27 | M4/M5 implementation | same |
| 2026-11-27 | Pre-M6 fixes and M6 (checkpoint 7.18) | IDE spec task execution, sub-agents |
| 2026-11-28 | M7 production readiness (final checkpoint) | Kiro agent (spec task execution subagent, IDE) |

The order is requirements → design → tasks → task execution by delegated subagents, milestone by milestone (M0 to M7). The build log records two interrupted subagent runs that were re-run (task 2.33, network failure; task 3.15, "input stream interrupted"), and one task execution that was only partly done (5.4 and 5.7 stayed open after M4/M5, 5.4 was finished in M6).

The build log has no dated entries for work after the M7 checkpoint. Later commits (for example `a2f6119` write-token capability, `d4f140b`, `2ce9c05`) are in git but are **not** described as Kiro sessions in the build log, so we do not label them as Kiro evidence. The build log's "Evidence checklist" still shows "First actual Kiro implementation commit", "Working public demo" and "Submission write-up and public demo video" unchecked, and the user should update it before submitting.

## Requirement → design → test traceability

- The design numbers 41 correctness properties. The test code carries the tag `Feature: parallel-simulation-mvp, Property N` for **33 distinct properties** (33 occurrences in 33 files):
  `grep -rhoE 'Feature: parallel-simulation-mvp, Property [0-9]+' backend web --include='*.go' --include='*.ts' --include='*.tsx' --exclude-dir=node_modules | sort -u | wc -l`
- There are 40 `func TestProp*` property tests in `backend` (`grep -rhoE 'func TestProp\w+' backend | wc -l`).
- The 11 optional property sub-tasks that were not done (2.8, 2.28, 2.29, 3.10, 7.3, 7.5, 7.13, 7.14, 10.2, 10.5, 10.8) are the likely gap between 33 and 41, but we did not map each of the 8 untagged properties individually, so this is an inference.
- `tasks.md` states which tasks map to which requirement and invariant, with a "Verify:" command per task.

## Where the spec was corrected after the fact

`tasks.md` has "Reconciliation (post-redesign / M4/M5 pass / M6 pass / M7 pass)" notes under many tasks: they record what was delivered, what remains and where the delivered code deviates from the plan. Examples: task 5.1 (SSE) first "Remaining", later "Delivered: `backend/internal/httpapi/sse.go`". The "Notes" section at the end records deviations (LLM-under-lock fix, calibration changes, bugs found by tests). The build log "Known issues" fields carry the same record.

## Kiro features: used and not used

| Feature | Evidence |
|---|---|
| Steering files | Used: 7 files in `.kiro/steering/` |
| Specs (requirements, design, tasks) | Used: `.kiro/specs/parallel-simulation-mvp/` |
| Delegated task execution (subagents) | Used per the build log (M0 to M7 sessions) |
| Property-based test generation guided by design properties | Used (tags above) |
| Hooks | **Not used** (no hook files) |
| MCP servers | **Not used** (no MCP config in `.kiro/`) |
| Checkpoints (Kiro IDE restore points) | No evidence in the repository. "Checkpoint" tasks in `tasks.md` (4.9, 6.12, 7.18) are plan milestones for running every check, not a Kiro feature claim. |

## Key milestone commits

| Commit | Date | Subject |
|---|---|---|
| `ddc7c1d` | 2026-11-26 | M0 repository scaffolding |
| `d44bf1b` | 2026-11-26 | M1 vertical slice |
| `281dd44` | 2026-11-27 | redesigned studio |
| `78d3da3` | 2026-11-27 | M2 complete engine, golden hashes |
| `30ef0d1` | 2026-11-27 | M3 persistence, snapshots; fix State.clone aliasing |
| `a102b29` | 2026-11-27 | security patch for pgx and x/text |
| `744607a` | 2026-11-27 | M4 completion, M5 LLM manager and duel |
| `3dc5028` | 2026-11-27 | M6 diff, ensembles, calibration; LLM calls outside lock |
| `c1e0024` | 2026-11-28 | conservative LLM token reservation and limits |
| `2bece20` | 2026-11-28 | FramesRange (one replay per range) |
| `621ec72` | 2026-11-28 | HTTP hardening |
| `6ff54e5` | 2026-11-28 | flagship demo path e2e, demo script |
| `98169a5` | 2026-11-28 | M7 notes, tasks reconciliation, build log |
| `a2f6119` | 2026-11-28 | write-token capability and LLM access code |
| `2ce9c05` | 2026-11-28 | e2e screenshot wait, demo-numbers token (latest at writing) |

`git log --oneline | wc -l` returned 27 at writing.

## What judges can verify

- `.kiro/steering/`, `.kiro/specs/parallel-simulation-mvp/{requirements,design,tasks}.md`
- `docs/kiro-build-log.md`, `docs/calibration.md`, `docs/benchmarks.md`, `docs/demo-script.md`, `docs/ci.md`
- Tests: `cd backend && go test ./...` (PostgreSQL tests need the compose database; see README), `cd web && npm run test`, `npm run e2e`
- GitHub Actions run 46100197654 (`gh run view 46100197654`): jobs web, openapi lint, go (arm64), go (amd64), docker build, e2e all passed. `docs/ci.md` and the build log still say the workflow had not run; they are stale on this point.
- `git log --stat` for the milestone commits above.
