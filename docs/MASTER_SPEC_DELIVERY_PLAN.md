# Delivery Plan — AI CS + NOC L1 Master Specification

Status: active. Source of truth for target behavior: `AI_AGENT_HERMES_MASTER_SPEC.md`.

## Guardrails

- No external write action is enabled until policy, approval, idempotency, and verification gates are executable and replay-tested.
- Existing external adapters remain disabled and read-only unless an operator explicitly enables a staged integration.
- PostgreSQL is the transactional source of truth in production; JSON stores are development fallback only.
- A case cannot be marked `RESOLVED` without a recorded successful verification.
- Network cases escalate to `noc_senior`; non-network cases escalate to `admin`. Human decisions override AI recommendations.

## Phase 0 — Secure baseline

**Tasks**
- Bind locally by default and require an explicit production exposure mode.
- Add authenticated operator identities, RBAC, CSRF for browser mutations, webhook authentication, request limits, and audit actor/trace records.
- Disable unrestricted Codex execution by default; remove Codex as a human-escalation substitute.
- Restrict manual diagnostics to authorized, network-allowlisted targets.

**Tests / exit gate**
- Every operational API rejects unauthenticated requests.
- Customer webhook rejects unsigned traffic.
- Secrets never appear in API output, logs, audit payloads, prompts, or fixtures.
- No write tool can execute.

## Phase 1 — Case Engine and durable event core

**Tasks**
- Create a `case` domain with legal state transitions, stable `CASE-YYYYMMDD-XXXXXX` IDs, versioning, and resolution-verification invariant.
- Add conversations/messages, cases, case events, escalation requests, human decisions, audit/outbox, idempotency, and lease tables in additive migration `002_case_engine.sql`.
- Wire PostgreSQL repositories/migration runner and retain JSON only as development fallback.
- Route WhatsApp ingestion through idempotent message-to-case service.

**Tests / exit gate**
- Duplicate message IDs create one message/case/workflow run across restart.
- Conflicting workflow lease is rejected/recorded.
- Illegal state transitions and `RESOLVED` without verified result are rejected.
- Case event/audit/outbox writes are one transaction.

## Phase 2 — Conversation AI and customer context

**Tasks**
- Split conversation orchestration from technical reasoning.
- Add intent/domain taxonomy, per-intent required fields, sufficiency engine, `WAITING_CUSTOMER`, and customer-safe fact projection.
- Resolve WhatsApp identity to customer/service/device aliases without asking customers for API-obtainable data.

**Tests / exit gate**
- New conversations transition deterministically to `READY_FOR_DIAGNOSIS` only when required data exists.
- Customer replies never contain internal credentials, hidden reasoning, raw tool output, or unverified claims.

## Phase 3 — Typed read-only evidence platform

**Tasks**
- Change adapters to return typed evidence, source timestamps, structured errors, and minimized PII.
- Add customer-to-billing/PPPoE/router/ONT resolution.
- Enforce workflow evidence, timeout, retry, audit, and verification metadata at runtime.
- Enable one staged read-only adapter at a time after contract validation.

**Tests / exit gate**
- Each enabled adapter has staging contract tests and semantic authenticated health checks.
- Missing evidence remains `UNKNOWN`; no diagnosis fabricates a live state.

## Phase 4 — Policy, approval, action, and verification

**Tasks**
- Add staff directory, availability, permissions, escalation routing, acknowledgement/fallback, human decision loop, approval tokens, execution records, and verification engine.
- Enforce risk tiers including `CRITICAL`, role, expiry, retry ceiling, rollback metadata, and evidence prerequisites.
- Implement durable outbox for customer and human notifications.

**Tests / exit gate**
- Network routes only to NOC Senior; non-network routes only to Admin.
- An approval binds exact case/action parameters and expires.
- Successful external response with failed verification never resolves the case.

## Phase 5 — Controlled remediation pilot

**Tasks**
- Add exactly one reversible, customer-scoped write capability behind feature flag after Phase 4 passes.
- Enforce idempotency key, action lease, policy, approval, retry limit, audit, and post-action verification.

**Tests / exit gate**
- Duplicate/concurrent requests cause at most one external mutation.
- Verification failure becomes `FAILED` or escalation, never `RESOLVED`.

## Phase 6 — Correlation, learning, observability, and hardening

**Tasks**
- Correlate cases by time, area, router, OLT, PON, and upstream into mass incidents.
- Add validated knowledge candidates/review/approval and customer feedback.
- Add metrics, traces, alerts, dependency semantics, KPI APIs, retention, replay/chaos/race/security testing.

**Tests / exit gate**
- A correlated outage suppresses redundant deep diagnosis.
- Learning cannot self-promote into production knowledge.
- Replay suite covers delayed/reordered/duplicate messages, dependency failure, gateway outage, and concurrent cases.

## Traceability

| Master-spec area | Delivery phase |
|---|---|
| Case lifecycle, transactional truth, duplicate protection | 1 |
| Conversation sufficiency and customer safety | 2 |
| Evidence reasoning and external read APIs | 3 |
| Policy/risk/approval/verification/human authority | 4–5 |
| Incident correlation, learning, KPI, production hardening | 6 |
