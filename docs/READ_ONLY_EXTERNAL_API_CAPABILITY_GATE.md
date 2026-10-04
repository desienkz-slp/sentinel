# Read-only External API Capability Gate

## Aturan

Tidak ada API external yang dianggap aman hanya karena endpoint dapat diakses. Setiap capability harus diklasifikasikan:

- `VERIFIED`: bounded, read-only, adapter/registry parity, schema dan redaction terbukti.
- `IMPLEMENTED_NOT_EXPOSED`: ada di source tetapi belum terbukti dispatchable/deployed.
- `BLOCKED`: filter, projection, credential, mapping, atau contract belum cukup.
- `FORBIDDEN_WRITE`: semua mutasi.
- `FORBIDDEN_SENSITIVE_READ`: GET yang mengembalikan password, secret, private key, VPN/shared secret.

## Capability yang aktif saat inventory dibuat

Registry produksi memiliki 12 READ aktif dan satu WRITE tetap nonaktif (`mikrotik.disconnect_pppoe`). Adapter domain: Billing, RADIUS, MikroTik, GenieACS.

| Domain | Capability read-only | Status operasional |
|---|---|---|
| Billing | customer, history, list customer | adapter/registry parity dan bounded pagination harus diuji sebelum klaim full capability |
| RADIUS | session, user, system stats | system stats verified; session/user diblokir dari live contract sampai server-side filtering ada |
| MikroTik | PPPoE, interface stats/live, queue traffic | health verified; per-customer/queue harus server-filtered dan identity required |
| GenieACS | device state, device list | aggregate/device query terbatas aman; bulk list diblokir sampai pagination/projection/redaction terbukti |

## Remediasi yang sudah diterapkan

`radius.get_user` sebelumnya dapat menyimpan password cleartext dari Radius API pada `tool.Output.Data`. Perbaikan commit `7133d69` hanya mengembalikan projection aman dengan `password_set`, tidak pernah nilai password.

## Contract gates sebelum mengaktifkan capability baru

1. Manifest read-only deny-by-default: method/path/sentence, tool, argumen wajib, limit row/byte, schema, sensitivity.
2. Registry/adapter parity: setiap READ enabled di registry harus diiklankan dan diimplementasikan tepat satu adapter.
3. Method gate: hanya HTTP GET atau RouterOS `print`/`monitor-traffic once` dari allowlist.
4. Bounded identity/projection: tidak boleh bulk list tanpa server filter/projection/pagination.
5. PII/secret gate: output, audit, error, dan report tidak boleh memuat password/token/secret/private key/customer identifiers/IP/MAC/device IDs.
6. Fixture contract: success/empty/401/403/404/malformed/timeout/schema drift.
7. Live smoke opt-in: `NOC_CONTRACT_LIVE=1`, satu probe per capability, nonce, allowlist test identity/interface/device, output hanya schema hash/latency/status bucket.

## Status BLOCKED yang benar

- RADIUS `/users` dan `/sessions`: endpoint bulk sampai ada filter upstream atau adapter safe projection yang tidak mendownload seluruh data.
- MikroTik PPP/queue: bulk read tetap BLOCKED sampai filter RouterOS server-side wajib. Pengecualian terbatas `mikrotik.get_customer_traffic` memakai exact `?name=<identity>` pada `/ppp/active/print`, lalu tepat satu `/interface/monitor-traffic once` untuk interface PPPoE yang ter-resolve unik; tidak ada fallback queue.
- `mikrotik.get_pppoe_interface_traffic` tetap capability masa depan yang belum diimplementasikan dan deny-by-default; traffic pelanggan yang aman menggunakan `mikrotik.get_customer_traffic` di atas.
- GenieACS device-list: bulk output sampai pagination, aggregate mode, dan ID redaction tersedia.
- Billing history/list: gunakan setelah parity binary+registry dan bounded page/filter terbukti.
- Semua write dan sensitive-read tetap forbidden.
