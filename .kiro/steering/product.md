---
inclusion: always
---

# Product steering

## Identity and status

Repository: **KiroFrankenstein**. Hackathon category: **Frankenstein** (combine distinct technologies in a useful application).

**Provisional product concept:** **Incident Stitcher**. This is not a final product decision. Before writing app code, confirm the direction and update this file if the product changes.

## Problem

During an incident, developers manually reconcile code-change records, Kubernetes events, and distributed traces across disconnected tools. They need a fast, evidence-backed view of what changed and what failed.

## Audience

Platform engineers, SREs, and backend developers investigating an incident.

## Value proposition

Turn disconnected incident signals into one timestamp-normalized timeline and evidence-linked candidate explanations. Never describe a correlation as a proven root cause.

## MVP outcome

A judge can load a provided example incident, see all three source types in a unified timeline, select a candidate hypothesis, inspect its supporting and conflicting evidence, and understand uncertainty without entering credentials.

## Principles

- Working end-to-end demo before adding sophisticated AI features.
- Deterministic, reproducible correlation before optional LLM summarization.
- Every explanation links to exact input evidence.
- Honest uncertainty; avoid invented facts.
- Accessible, polished UI; no spooky styling at the expense of utility.
- Never copy code from existing personal projects without disclosure and license review.

## Exclusions for MVP

No production cluster credentials, write access to GitHub, automatic remediation, automated PR merges, or security-sensitive log retention.
