# Implementation tasks: Incident Stitcher MVP

**Status:** All tasks pending. This is a proposed Kiro work plan, not completed engineering work.
**Prerequisite:** Approve or revise the product concept and MVP spec before implementation.

- [ ] 1. Confirm concept and freeze MVP boundaries
  - Review product, technology, and structure steering in Kiro.
  - Decide whether Incident Stitcher is the final hackathon idea.
  - Resolve the open decisions in requirements.md and design.md.

- [ ] 2. Bootstrap the application
  - Initialize a strict TypeScript + Next.js app.
  - Add linting, unit testing, and scripts for local dev/test/build.
  - Confirm `npm run build` and relevant checks pass.

- [ ] 3. Create a synthetic incident dataset
  - Generate connected GitHub, Kubernetes, and OpenTelemetry JSON fixtures.
  - Include a plausible service deployment and subsequent observable failures.
  - Keep all sample data artificial, without keys or real customer records.

- [ ] 4. Implement source parsers and canonical normalization
  - Add Zod schemas, per-source adapters, timestamp handling, and stable evidence IDs.
  - Reject invalid inputs with explicit errors and warnings.
  - Test source differences, ordering, missing timestamps, and malformed payloads.

- [ ] 5. Add deterministic correlation rules
  - Implement candidate hypothesis generation with cited evidence IDs.
  - Add insufficiency and conflicting-evidence behavior.
  - Test repeatability and false-positive guardrails.

- [ ] 6. Implement case loading/query endpoints
  - Support built-in fixture case and bounded JSON import.
  - Select storage model compatible with chosen deployment.
  - Add data validation and error handling tests.

- [ ] 7. Build the investigation UI
  - Responsive overview, timeline, filters, and evidence-linked hypothesis detail.
  - Label synthetic data and inferred claims clearly.
  - Cover loading, empty, and failure states.

- [ ] 8. Test and demonstrate end-to-end behavior
  - Add an E2E test: load fixtures -> timeline -> hypothesis -> evidence.
  - Check lint, build, test, accessibility basics, and runtime errors.
  - Deploy a public working demo without required secrets.

- [ ] 9. Capture genuine Kiro engineering evidence and submit
  - Record actual Kiro spec iteration, agents/hooks/CLI/IDE usage in docs/kiro-build-log.md.
  - Document local setup, deployed URL, limitations, and screenshots.
  - Record a clear demo video under three minutes and complete Devpost submission.

## Definition of done

A fresh visitor can open the public demo, load the synthetic case, see all three technology sources, inspect the cross-source timeline, understand a labeled hypothesis with the precise supporting evidence, and reproduce the result locally with automated tests.
