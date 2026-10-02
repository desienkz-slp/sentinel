# Rencana: Menghubungkan Orchestrator Layer ke Alur WhatsApp

> Tujuan: membuat alur runtime NOC Sentinel **benar-benar** sesuai master spec
> `AI_AGENT_HERMES_MASTER_SPEC.md`, bukan hanya "package ada tapi tidak dipanggil".
>
> Rencana ini adalah **acuan tunggal** untuk 6 fase koneksi orchestrator layer.
> Aturan: SETIAP fase dikerjakan di NEW SESSION terpisah (1 session = 1 fase).

---

## Status fase

| Fase | Isi | Status |
|---|---|---|
| 1 | Wire Case Engine (state machine + Case ID) | ✅ selesai (commit `edc162b`, `7b534be`) |
| 2 | Wire Escalation Engine (handoff ke NOC/Admin) | ✅ selesai (commit `a2866c1`, `a91258e`, `18f54bb`) |
| 3 | Wire Policy gate untuk execute (deny-by-default) | ✅ selesai (commit `7da1e70`) |
| 4 | Wire Verification Engine (resolved wajib verifikasi) | ⬜ belum |
| 5 | Observability & audit lengkap (lifecycle + KPI) | ⬜ belum |
| 6 | Deploy + validasi end-to-end | ⬜ belum |

---

## 1. Target arsitektur (master spec §2, §5)

```
Customer (WA)
  → Conversation AI  (klasifikasi intent, kumpulkan info)   [Fase 1 ✓]
  → Case Engine      (buat Case, transisi state)            [Fase 1 ✓]
  → Reasoning AI     (workflow diagnosis)                    [SUDAH sejak awal]
  → Policy/Risk      (gating untuk execute)                 [Fase 3]
  → Execute / Escalate                                      [Fase 2 ✓ / Fase 3]
  → Verification                                            [Fase 4]
  → Conversation AI  (balasan ke customer)                   [SUDAH]
```

---

## 2. Nomor eskalasi (sudah terisi di config 133)

- NOC Senior (network authority): `6281333678765`
- Admin (non-network authority): `6285606311311`

---

## 3. Rincian fase tersisa

### FASE 3 — Wire Policy gate untuk execute
- Aktifkan `shouldEscalate`/action-gating berbasis `internal/policy` (bukan hardcode `return false`).
- Alur: `ACTION_PROPOSED → POLICY_CHECK` → LOW execute (jika policy izinkan), MEDIUM/HIGH/CRITICAL → ESCALATION.
- `policies/default-policy.yaml` deny-by-default untuk write → pengaman.
- Test: action WRITE ditolak tanpa approval.

Status: ✅ selesai. Implementasi:
- `internal/agent/policywire.go` — `PolicyGate` (satu-satunya jalur keputusan eksekusi): `Check()` memanggil `ExecutionGuard.Authorize` (allowlist dari registry + probe diag read-only, binding approval, tier risiko, mode COPILOT). Hanya `ALLOW` yang diteruskan ke dispatcher; `APPROVAL_REQUIRED`/`DENY` → `Escalate()`: audit append-only + case `ACTION_PROPOSED → POLICY_CHECK → ESCALATION` (handoff manusia fase 2), TIDAK dieksekusi dan TIDAK didelegasikan ke Codex/model lain (phase 0).
- `shouldEscalate` kini berbasis policy gate (bukan hardcode `false`); eskalasi kebijakan menandai laporan + mengandalkan `CaseState==ESCALATION` untuk handoff.
- Gate dipasang di DUA jalur: loop tool-call LLM (`agent.go`) dan workflow deterministik (`workflow.go`) — keduanya mengecek SEBELUM invoke apa pun.
- Wiring di `main.go`: `ExecutionGuard` dari registry + diag tools, `engine.Policy` terpasang.
- Catatan: pembacaan diagnostik LOW dieksekusi inline sebagai bagian REASONING (bukan aksi remediasi), sehingga alur INVESTIGATION/ESCALATION diagnosis tetap utuh.

### FASE 4 — Wire Verification Engine
- Setelah action: `EXECUTING → VERIFYING`.
- `RecordVerification` (query ulang sistem → cek expected state), hanya `Passed=true` izinkan `RESOLVED`.
- Gagal → `FAILED → ESCALATION`.
- Test: RESOLVED ditolak tanpa verifikasi.

### FASE 5 — Observability & audit
- Dashboard tampilkan Case lifecycle (state, events, escalation target).
- Audit log: transisi state, policy decision, escalation, verification.
- KPI (§40) terisi dari case engine.

### FASE 6 — Deploy + validasi end-to-end
- Build Linux → deploy 133 (git pull + binary).
- Validasi: WA lokal (nurma) → cek Case ID, state, escalation, verification.

---

## 4. Prinsip lintas fase

- Standar ditegakkan di KODE (bukan prompt) — tiap fase punya test yang mengunci perilaku.
- Tiap fase additive, backward-compatible, tidak merusak alur yang sudah jalan.
- Tiap fase menutup loop: `go test ./...` lulus → commit + push GitHub → server 133 pull → verify nyata.
- Server 133 deploy: cross-compile Linux di PC → scp binary (backup dulu) → systemctl restart → verify.
- Audit secret sebelum push: jangan commit nomor nyata/token di fixture test (pakai nilai sintetis).
