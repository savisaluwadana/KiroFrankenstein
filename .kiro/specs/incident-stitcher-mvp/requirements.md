# Requirements: Incident Stitcher MVP

**Status:** Draft / provisional product direction; review in Kiro before implementation.

## Objective

Give a platform engineer a reproducible incident-investigation demo that combines three otherwise disconnected evidence sources: GitHub, Kubernetes, and OpenTelemetry.

## Functional requirements

### R1: Safe sample data import

**User story:** As an investigator, I want to load an example incident without entering production credentials so that I can evaluate the tool.

**Acceptance criteria**
1. WHEN the user chooses the provided demo incident, THE SYSTEM SHALL load a bounded synthetic dataset representing all three source types.
2. WHEN the user imports a supported JSON file, THE SYSTEM SHALL validate its type and structure before persistence.
3. IF a file is invalid, oversized, or unsupported, THE SYSTEM SHALL show a recoverable error without importing partial or untrusted records.

### R2: Normalized evidence timeline

**User story:** As an investigator, I want a unified time-ordered view so that I can compare signals across tools.

**Acceptance criteria**
1. WHEN data is imported, THE SYSTEM SHALL map each supported record into a canonical event with a stable evidence ID, source, service, event type, UTC timestamp, and human-readable summary.
2. THE SYSTEM SHALL preserve original source timestamps and a pointer to the original record.
3. WHEN I filter by source or service, THE SYSTEM SHALL update the timeline without modifying the underlying evidence.
4. IF timestamps are missing or cannot be parsed, THE SYSTEM SHALL visibly flag those records instead of guessing times.

### R3: Evidence-based hypotheses

**User story:** As an investigator, I want candidate incident explanations backed by specific data so that I can prioritize investigation.

**Acceptance criteria**
1. WHEN an incident has enough valid events, THE SYSTEM SHALL identify at least one deterministic temporal or service-based correlation using an explainable rule.
2. THE SYSTEM SHALL label every candidate as a **hypothesis**, not a confirmed root cause.
3. WHEN a user opens a hypothesis, THE SYSTEM SHALL display the exact evidence IDs and the rule that produced it.
4. IF there is insufficient evidence, THE SYSTEM SHALL explicitly say so instead of inventing an explanation.

### R4: Complete investigation view

**User story:** As an evaluator, I want to inspect a real working flow quickly.

**Acceptance criteria**
1. THE SYSTEM SHALL provide a responsive screen for data sources, a timeline, and a hypothesis-detail view.
2. WHEN the demo dataset loads, THE SYSTEM SHALL make the end-to-end investigation available without authentication or external API keys.
3. THE SYSTEM SHALL identify synthetic demo data as synthetic, not live production signals.

### R5: Security, privacy, and reproducibility

1. THE SYSTEM SHALL reject unsupported file types and enforce import-size/record limits.
2. THE SYSTEM SHALL avoid live production credentials and automatic external write actions in MVP.
3. THE SYSTEM SHALL provide tests for timestamp normalization, source parsing, hypothesis generation, and invalid inputs.
4. THE SYSTEM SHALL ship with documented local setup and a public deployed demo by submission.

## Out of scope

Live Kubernetes access; autonomous incident remediation; direct GitHub mutations; paid or always-online model inference; full-scale observability backend.

## Open decisions to resolve before code

- Confirm whether Incident Stitcher is the final concept.
- Choose a sustainable persistence method for deployment.
- Agree exact source fixture schemas and an example incident narrative.
- Define the deployed demo environment and its data retention policy.
