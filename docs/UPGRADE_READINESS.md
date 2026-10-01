# NOC Sentinel — Upgrade Readiness

## Status

- **Authoritative reference:** [`../Hermes_Autonomous_NOC_Master_Blueprint.md`](../Hermes_Autonomous_NOC_Master_Blueprint.md)
- **Operating principle:** `OBSERVE → CORRELATE → DIAGNOSE → PLAN → POLICY CHECK → ACT → VERIFY → DOCUMENT → LEARN`
- **Deployment mode until further notice:** **COPILOT / READ_ONLY**. No network, billing, RADIUS, GenieACS, or MikroTik mutation may be enabled by model output alone.

## What Already Exists

| Blueprint area | Current implementation | Status |
|---|---|---|
| WhatsApp + Web UI ingress | Embedded Baileys gateway and one dashboard | Ready |
| Intent gate | Deterministic `CHAT / INFO / UNCLEAR / COMPLAINT` gate | Ready |
| Basic diagnostics | Whitelisted ping, DNS, TCP, HTTP, traceroute, RADIUS/interface/service/system probes | Ready |
| Per-sender history/cache | In-process session and file-backed memory | Transitional |
| LLM gateway | OpenAI-compatible LLM client through 9Router | Ready |
| Incident storage / audit | JSON memory only | Upgrade required |
| Policies / approvals | Not yet enforced for operational mutations | Upgrade required |
| API adapters | None for Billing/RADIUS/GenieACS/MikroTik | Awaiting endpoint contracts |
| Redis/PostgreSQL/pgvector | Infrastructure definition included; services not yet provisioned | Prepared |

## Upgrade Stages

### Stage 0 — Foundation (prepared now)

- Version-controlled policies, skills, runbooks, workflows, schemas, tool registry, database migration, replay/security fixtures, and Compose infrastructure.
- No production action enabled.
- Docker daemon must be running before local PostgreSQL/Redis can be provisioned.

### Stage 1 — Data & Audit

- Provision PostgreSQL + Redis from `deploy/noc-infra.compose.yml`.
- Apply `migrations/001_autonomous_noc.sql`.
- Persist incident, evidence, tool calls, workflow runs, approvals, and audit events in PostgreSQL.
- Keep Redis only for TTL cache, locks, deduplication and short-lived working state.

### Stage 2 — Official Read-Only Tool Domains

Implement adapters only after receiving each API contract and least-privilege credential:

1. Billing — customer identity and account status.
2. RADIUS — authentication/session status.
3. MikroTik — PPPoE/interface operational state.
4. GenieACS — CPE/ONT state.

Each adapter must implement the tool contract, timeout, structured error, health check, request ID, and read-only tests before registration.

### Stage 3 — Identity + Correlation

- Normalize phone/customer/PPPoE/device identities in `customer_identities`.
- Resolve a customer once, then correlate Billing + RADIUS + MikroTik + GenieACS evidence.
- Unknown data remains `UNKNOWN`; unavailable services never become inferred success.

### Stage 4 — Deterministic Workflow

Implement `CUSTOMER_INTERNET_DOWN` from `workflows/customer-internet-down.yaml`:

`identity → Billing → RADIUS → MikroTik → GenieACS → correlation → diagnosis → incident/audit → customer response`

The LLM may summarize evidence and select a read-only skill; deterministic code controls sequence and required evidence.

### Stage 5 — Policy / Approval / Controlled Action

- Use `policies/default-policy.yaml` as the baseline.
- Enable only one scoped remediation at a time, beginning with **stale PPPoE recovery**.
- Require policy decision, idempotency key, audit record, approval when needed, verification, and rollback/elevation path.
- Never enable global autonomous write access.

### Stage 6 — Learning, Replay, Observability

- Promote only verified outcomes into learning candidates.
- Human review is required before changing a runbook/policy.
- Add replay, failure, prompt-injection and regression suites.
- Export structured logs/metrics/traces with `request_id`, `trace_id`, `incident_id`.

## Non-Negotiable Gates

1. No secret in Git, prompt, skill, memory, incident summary, or vector index.
2. No write-capable tool without policy, scope, idempotency, audit, verification, and rollback definition.
3. No production integration test against a real mutation endpoint.
4. No incident becomes `RESOLVED` without service-state verification.
5. Every new adapter must have a `/health` contract and explicit source-of-truth definition.
6. Every configuration change must be reproducible from version-controlled artifacts plus non-versioned secrets.

## Current Blockers Requiring Operator Inputs

The repository deliberately does **not** invent these values:

- Billing API base URL, auth method, read-only customer/account endpoints.
- RADIUS API base URL, auth method, session lookup contract.
- MikroTik access model and read-only endpoints/permissions.
- GenieACS base URL, API version, read-only device/ONT lookup contract.
- Production/staging network boundaries, TLS certificates, data retention and operator roles.

Until these are supplied, tools remain declared-but-disabled rather than making unsafe or fabricated calls.

## Validation Commands

```bash
# Application baseline
./install.sh --check
go vet ./...
go test ./... -count=1

# Infrastructure manifest (Docker Desktop/daemon must be running)
docker compose --env-file deploy/.env -f deploy/noc-infra.compose.yml config

# Start local development infrastructure only after reviewing deploy/.env
docker compose --env-file deploy/.env -f deploy/noc-infra.compose.yml up -d
```
