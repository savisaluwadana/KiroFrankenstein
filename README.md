# KiroFrankenstein

An early-stage project for the **Kiroween 2026 Frankenstein** hackathon category.

> **Status:** Planning and Kiro scaffolding only. There is no runnable application yet.
> **Working concept (not finalized):** **Incident Stitcher**, an evidence-first incident investigation workbench.

## What we're proposing

Incident investigations span systems that do not speak the same language. Incident Stitcher would bring together **GitHub change/deployment records**, **Kubernetes event exports**, and **OpenTelemetry trace exports** into a single, normalized incident timeline. It would propose *evidence-linked hypotheses* rather than claiming to prove a root cause.

This is a provisional direction and can be changed before implementation. No existing project code is reused.

## Why the Frankenstein category?

The proposed product stitches together three different technical ecosystems:
1. GitHub commits and deployments, which describe code changes.
2. Kubernetes events, which describe workload and infrastructure behavior.
3. OpenTelemetry traces, which describe request-level runtime behavior.

The value comes from correlating them, explaining uncertainties, and enabling investigations across previously disconnected data.

## Proposed MVP

- Import small sample JSON files from all three sources; no credentials required for the demo.
- Normalize timestamps and data into a common event model.
- Show a sortable incident timeline with filters by source/service.
- Generate deterministic correlations, explicitly marked as hypotheses.
- Let a user inspect the evidence underlying each hypothesis.
- Provide a reproducible example incident, automated tests, and a deployed demo.

**Not yet in scope:** live cluster access, auto-remediation, production monitoring, and autonomous code changes.

## Proposed technology

Next.js + TypeScript, Zod validation, and a lightweight local database for persistence. Final dependencies should be confirmed during Kiro spec review before generating app code.

## Kiro artifacts

- [Product steering](.kiro/steering/product.md)
- [Technology steering](.kiro/steering/tech.md)
- [Project structure steering](.kiro/steering/structure.md)
- [MVP requirements](.kiro/specs/incident-stitcher-mvp/requirements.md)
- [MVP design](.kiro/specs/incident-stitcher-mvp/design.md)
- [Implementation tasks](.kiro/specs/incident-stitcher-mvp/tasks.md)
- [Kiro build log](docs/kiro-build-log.md)

### How to proceed with Kiro

1. Clone this repo and open it in **Kiro IDE** or **Kiro CLI**.
2. Review the provisional product direction; adjust the steering and spec if needed.
3. Ask Kiro to review the requirements/design, resolve ambiguities, and then execute the tasks in order.
4. Save genuine Kiro-generated specs, hooks, code, and reflections in version control. Document **actual** Kiro usage in the build log; these initial files were prepared as planning scaffolding, not evidence that Kiro executed them.
5. Run the app and tests, deploy a public demo, and create a demo video under three minutes.

## Hackathon readiness

- [x] Public GitHub repository
- [x] MIT open-source license
- [x] Root `.kiro/` directory
- [ ] Finalize the product concept
- [ ] Build and test the functional application **using Kiro**
- [ ] Deploy a working demo and publish testing instructions
- [ ] Capture the Kiro workflow evidence and submit the Devpost entry

The official rules: https://kiroween.devpost.com/rules

## License

MIT License. Copyright (c) 2026 Savi Saluwadana.
