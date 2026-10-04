# Phase 7 Blocker — Verified Monitoring Sources

Tanggal: 2026-10-04

## Status

Runtime has pure evaluators, durable monitoring store, and GET-only monitoring projections. It does **not** have a valid source producer. Enabling Q226/Q228 now would fabricate evidence, which is prohibited.

## Q226 recovery parent 90%

Existing billing/RADIUS/MikroTik/GenieACS adapters cannot currently establish all required facts together:

1. Immutable, paginated/snapshot-consistent active customer membership for a canonical node at parent-open time.
2. Authoritative customer-to-node and node-to-OLT/PON/NAS mapping/version.
3. Fresh, independent customer state re-read with `customer_id`, `node_id`, `ACTIVE|DOWN|UNKNOWN`, `observed_at`, source record ID, source, and TTL.

Billing router/area must not be re-labeled as verified PON. RADIUS session cleanup and GenieACS seven-day online inference are not valid independent fresh recovery evidence.

## Q228 uplink flap

The current MikroTik adapter reads current interface snapshots only. It does not provide immutable `event_id`, canonical uplink identity, source timestamp, replay cursor/sequence, or durable event history. Poll deltas cannot be represented as a canonical source unless a separate poll-derived contract is approved.

## Required read-only contracts

### Membership source

`Snapshot(node_id) -> {snapshot_id, node_id, observed_at, customer_ids, source, mapping_version, cursor/total}`

### Fresh recovery read

`Read(customer_id, node_id) -> {customer_id, node_id, ACTIVE|DOWN|UNKNOWN, observed_at, source, record_id, independent}`

### Uplink event source

`ReadAfter(cursor) -> {events:[{event_id,uplink_id,UP|DOWN,observed_at,source,sequence}], next_cursor, source}`

## Safety

All source adapters remain disabled/read-only until contract tests pass. A `parent_closure_eligible` finding never closes parent/case automatically. Flap finding remains dashboard/audit alert only.
