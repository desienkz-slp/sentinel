# Plan Fase Integrasi Monitoring Read-only

## Status awal

Evaluator Q226 (recovery massal) dan Q228 (uplink flap) sudah murni, teruji, dan dideploy, tetapi belum memiliki sumber evidence runtime. Karena itu keduanya belum boleh diklaim sebagai deteksi/pemulihan produksi aktif.

## Invarian

- Tidak ada adapter write, policy execution, remediation, WhatsApp notification, case transition, handoff closure, atau auto-resolution.
- Recovery 90% hanya menghasilkan `parent_closure_eligible` pada insiden induk; case pelanggan tetap wajib verification sendiri.
- Flap hanya menghasilkan finding/audit/dashboard alert read-only.
- Semua snapshot, event, finding, dan evaluation memakai data sintetis untuk test.

## Fase 5 — Monitoring Store persisten

1. TDD: test merah untuk immutable mass-parent snapshot, recovery evaluation round-trip, flap event/finding/cooldown round-trip, retention active parent, dan persistence error eksplisit.
2. Tambahkan store JSON atomic khusus monitoring di `internal/incident`, terpisah dari store Incident lama.
3. Simpan parent snapshot/evaluations dan flap state/events/findings. Audit tetap trace tambahan, bukan state source.
4. Tambahkan canonical ordering `UplinkID → ObservedAt → EventID` sebelum evaluator flap digunakan runtime.
5. Test fokus + full suite + commit + push. Belum ada deployment wiring/source adapter.

## Fase 6 — Producer evidence terverifikasi

Buka parent sekali saja dari group korelasi dengan node/topologi dan snapshot membership terverifikasi. Jangan mengganti `Incident.Add` menjadi `Record` tanpa membuktikan suppress semantics tidak merusak diagnosis.

## Fase 7 — Read-only source dan evaluasi

Tambahkan source re-read customer state dan source event uplink yang canonical/read-only. Persist evidence, panggil evaluator, audit hasil. Tidak boleh ada action/mutasi.

## Fase 8 — Read-only projection

`GET /api/incidents/mass`, proyeksi `GET /api/alerts`, dan audit; semua route GET, backward-compatible.

## Evidence sebelum status selesai

- Reload/replay membuktikan snapshot immutable, dedupe event, cooldown, dan eligibility bertahan restart.
- Q226 synthetic replay: 90% eligible; 89%/UNKNOWN/stale/wrong node tidak.
- Q228 synthetic replay: 3 transition finding; duplikat/same-state/out-of-window/out-of-order/different interface tidak false positive.
- Tidak ada case/customer/incident-child auto-close atau network write.
