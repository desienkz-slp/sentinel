# Master Specification Acceptance Matrix

This matrix translates `AI_AGENT_HERMES_MASTER_SPEC.md` into verifiable release gates. A checkbox marked **foundation** means code exists but is not yet wired to production persistence or live adapters. A checkbox marked **blocked** cannot truthfully pass without operator-provisioned infrastructure/credentials and a controlled staging test.

| Area | Gate | Status | Evidence / required next proof |
|---|---|---:|---|
| Safety baseline | Default listener is loopback-only | pass | `config.Default()` uses `127.0.0.1:8090` |
| Safety baseline | Operational APIs and WhatsApp webhook require configured secrets even on loopback | foundation | `internal/security` + `server_security_test.go`; token guard has route coverage. RBAC, CSRF, request-rate limiting, actor/trace audit wiring, and production exposure gate remain required. |
| Safety baseline | Codex defaults to read-only sandbox | pass | config and bridge defaults |
| Case lifecycle | Case ID/state graph enforced | foundation | `internal/caseengine`, unit tests |
| Case lifecycle | Resolution requires recorded verification | foundation | `caseengine.Transition` rejects premature resolution |
| Durable core | Messages/cases/events/audit/outbox transactional in PostgreSQL | blocked | `002_case_engine.sql` exists; Docker unavailable and database credentials absent |
| Conversation | Required information is intent-specific; API facts not asked of customer | foundation | `internal/conversation` tests |
| Reasoning | Unknown evidence remains unknown | pass | `internal/correlation` existing tests |
| Tools | Registry/policy/adapter deny-by-default | pass | existing registry/policy/dispatcher tests; external tools remain disabled |
| External adapters | Billing/RADIUS/MikroTik/GenieACS contract tests against staging | blocked | endpoint, least-privilege credentials, and isolated staging path required |
| Human authority | Network→NOC Senior, non-network→Admin routing | foundation | `internal/escalation` tests; staff/on-call repository pending |
| Approval/action | Approval bound to action, idempotency, post-action verification | blocked | no production write adapter enabled by design |
| Reliability | Durable dedupe/lease/outbox/retry through restart | blocked | requires PostgreSQL/Redis and repository/worker implementation |
| Correlation | Multi-customer mass incident by topology/time | pending | current correlation is per-case evidence only |
| Knowledge | Candidate→human validation→approved knowledge | pending | automatic statistical playbook must not be promoted to truth |
| Observability | KPI/metrics/alerts dan health endpoints | foundation | `internal/observability`, `/api/metrics`, `/api/kpi`, `/api/alerts`, `/api/audit`, `/healthz`, `/readyz`; counter masih in-memory dan trace lintas layanan/persistensi metrik masih diperlukan untuk produksi. |
| Production hardening | Replay/chaos/race/security test suite | blocked | race test needs supported CGO compiler; live tests need isolated infrastructure |

## Required operator inputs before a production-live claim

1. PostgreSQL and Redis available in an isolated environment, with credentials stored only in `deploy/.env` or a secret manager.
2. Signed/secret-authenticated WhatsApp gateway callback configuration.
3. Named NOC Senior/Admin staff directory, on-call policy, and secure operator authentication source.
4. Staging endpoints plus least-privilege credentials for Billing, RADIUS, MikroTik, and GenieACS.
5. Explicit approved network allowlist for diagnostics and adapter connectivity.
6. Controlled replay fixtures containing only synthetic customer/device data.

Until those inputs and the blocked gates pass, the system must remain in read-only/COPILOT mode and all external registry tools remain disabled.
