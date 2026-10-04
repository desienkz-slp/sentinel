# MikroTik / RouterOS Native Binary API

NOC Sentinel uses the RouterOS **native binary API only**. It does not use RouterOS REST or the `www` / `www-ssl` services.

## Transport and login

- API: TCP port **8728** (plaintext).
- API-SSL: TCP port **8729** (TLS); a custom port may be configured.
- Commands are RouterOS binary API sentences: length-prefixed words terminated by `0x00`.
- RouterOS 6.43+ login: `/login =name=<user> =password=<pass>`.
- Older RouterOS login: challenge-response MD5.

The adapter is read-only. It has no RouterOS write commands and does not configure router services.

## Bounded read contract

Every customer or interface lookup is deny-by-default and bounded at the RouterOS query layer. A missing identity/interface is rejected before network I/O. Returned rows are checked again before output; a missing or non-unique response emits a bounded status and never a list.

| Tool | Required argument | Native RouterOS command | Bound | Output rule |
|---|---|---|---|---|
| `mikrotik.get_pppoe_status` | `identity` | `/ppp/active/print ?name=<identity>` | exact one PPPoE identity | at most one session; no prefix matching or session list |
| `mikrotik.get_interface_stats` | `interface` | `/interface/print ?name=<interface>` | exact one interface | at most one counter record; no interface list |
| `mikrotik.get_interface_live` | `interface` | `/interface/monitor-traffic =interface=<interface> =once=` | exact one interface | at most one live sample |
| `mikrotik.get_customer_traffic` | `identity` | `/ppp/active/print ?name=<identity>`, then `/interface/monitor-traffic =interface=<resolved-interface> =once=` | exact one PPPoE identity and resolved interface | at most one live sample |

## PPPoE customer traffic

Customer traffic is obtained only by resolving the exact active PPPoE session and sampling its resolved PPPoE interface once. RouterOS queue paths are not read or used:

- no `/queue/*` command;
- no simple-queue traffic source;
- no queue-derived customer list.

Ambiguous, missing, or mismatched sessions/interfaces are returned as `UNKNOWN` or a bounded no-data status. They never fall back to bulk reads.

## Registry and capability manifest

The release-controlled registry requires `identity` for PPPoE status and `interface` for both interface tools. The read-only capability manifest sets `max_rows: 1` for each bounded MikroTik capability. A tool absent from that manifest is blocked even if a local registry declaration is enabled.

## Security note

Plain API transmits modern RouterOS credentials in the clear; use API-SSL or a trusted private network. For API-SSL, deploy a verified router certificate where possible. The adapter itself does not enable services, change credentials, or issue write operations.
