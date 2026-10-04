# External API Hardening Report

Date: 2026-10-04  
Production: `root@172.18.20.137` / SSH alias `9router`  
Deployed revision: `0f3bb17`

## Completed

1. **Capability manifest gate** — external READ capability is deny-by-default. Missing, sensitive, write, unbounded, or adapter-mismatched capability fails closed.
2. **Billing** — exact identity required; customer lookup is one explicit page; ambiguous results fail closed; output is diagnostic-state projection only; history is blocked until safe upstream paging/projection is available; list returns aggregate metadata only.
3. **RADIUS** — cleartext password removed from tool data; only aggregate `radius.get_system_stats` remains safe/exposed. User/session bulk reads are blocked.
4. **MikroTik** — native binary API only; PPP status, interface stats, and PPPoE traffic require exact bounded identity/interface; customer traffic uses PPP active → PPPoE interface monitor once; Simple Queue is not used.
5. **GenieACS** — device state requires exact device ID and fixed projection/limit; bulk device list is disabled.
6. **Write tools** — remain disabled/forbidden.

## Production verification

- Service active.
- Health is ONLINE: LLM, WhatsApp, PostgreSQL, Redis are ONLINE.
- Read-only adapter health checks succeeded for Billing, RADIUS, MikroTik, GenieACS.
- Full local test suite and `go vet ./...` passed before deployment.

## Deliberately BLOCKED

- Billing history until bounded upstream projection/pagination is confirmed.
- RADIUS sessions/users until server-side exact filter/projection exists; no bulk download/filter is accepted.
- GenieACS device bulk list until pagination/aggregate output and redaction are enforced.
- Sensitive GET endpoints (passwords, NAS/VPN/WireGuard secrets) and all state-changing routes.

## Operational meaning

The production registry may still list older enabled READ declarations from its local overlay, but the runtime capability gate is authoritative: only manifest-approved, bounded, non-sensitive READ operations can invoke an external adapter. All other reads return a safe blocked/UNKNOWN outcome rather than broadening scope or exposing data.
