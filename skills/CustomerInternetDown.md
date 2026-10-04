# CUSTOMER_INTERNET_DOWN — Deterministic Read-Only Investigation Skill

## Trigger
Intent: `CUSTOMER_INTERNET_DOWN` with low/normal severity

## Purpose
Investigate customer internet loss using correlated evidence from authoritative sources. No write actions permitted at MVP stage. Returns a single recommended action (escalate/approved remediation).

## Required Context
- Normalized identity (phone number) or known customer/device mapping
- At least one of the following must be available and verified healthy: Billing, RADIUS, MikroTik, GenieACS

## Tools (READ-ONLY only; disabled until contract + creds provided)
1. `billing.get_customer(identity)` — account status
2. `mikrotik.get_pppoe_status(customer_or_device)` — PPPoE state
3. `genieacs.get_device_state(device_id)` — ONT/router state

## Diagnostic Sequence
1. Resolve customer identity to known identity ID (lookup via customers table).
2. Correlate evidence:
   - BILLING_ACTIVE = `account_status == ACTIVE`
   - DEVICE_ONLINE = `device.state in [ONLINE, READY]`
   - PPPOE_STALE = `pppoe_exists but NOT active`
3. Normalize evidence into canonical states.
4. If any required system unavailable → `UNKNOWN` for that domain, do not infer.

## Decision Logic
| Evidence Set | Diagnosis | Confidence | Recommendation |
|--------------|-----------|------------|----------------|
| Billing=ACTIVE, Device=ONLINE, PPPoE=OFF | PPPoE issue | HIGH | escalate |
| Billing=ACTIVE, Device=OFFLINE | CPE/ONT power/link issue | HIGH | escalate |
| Any component UNKNOWN | Insufficient data | LOW | ask for more info |

## Risk Level
LOW (read-only investigation only). Action proposal goes through policy engine.

## Verification
Before closing incident as RESOLVED, verify service state post-action.

## Escalation Path
If confidence < 0.8 OR ANY required source unavailable OR multiple components degraded → create escalation event with full evidence summary.

## Expected Response
Polite NOC-style reply containing current finding, missing data, recommended next step. Never claim certainty when evidence is insufficient.
