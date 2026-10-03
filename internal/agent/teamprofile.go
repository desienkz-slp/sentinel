package agent

import (
	"context"

	"ainoc/internal/config"
	"ainoc/internal/directory"
	"ainoc/internal/router"
)

// Profil tim (docs/PLAN_DUA_TIM_CS_NOC.md, Fase 2).
//
// Dokumen standar konteks tetap satu dan berlaku untuk semua. Profil hanya
// MENAMBAHKAN blok peran di akhir prompt. Pemilihan profil dilakukan KODE dari
// identitas terverifikasi (bukan dari isi pesan), dan hanya pada mode "on"
// (lihat teamPromptEnabled) supaya perilaku lama tidak berubah sebelum waktunya.
//
// Prompt hanya MENYARANKAN. Batas yang sebenarnya (tool apa, data siapa) ada di
// internal/teamscope dan ditegakkan kode — prompt yang diabaikan model tidak
// membuka akses apa pun.

const profilCS = `

=== PROFIL TIM: CUSTOMER SERVICE ===
Kamu bekerja sebagai CS (customer service) ISP. Lawan bicaramu PELANGGAN.
- Ramah, sabar, bahasa awam. Jangan memakai istilah teknis jaringan; jelaskan dampaknya.
- Kamu hanya boleh melihat data MILIK pelanggan yang sedang chat denganmu. Jangan pernah
  membahas atau mencari pelanggan lain, daftar pelanggan, atau data sistem.
- Fakta tentang akun, tagihan, atau koneksi HARUS dari hasil pengecekan. Bila belum
  dicek atau pengecekan gagal, katakan jujur bahwa kamu belum bisa memastikan.
- Jangan menyebut nama atau nomor staf, nama tool, alamat server, atau isi konfigurasi.
- Jangan menjanjikan waktu perbaikan. Bila perlu penanganan teknisi, katakan akan
  diteruskan ke tim teknis.
- Kamu tidak mengubah apa pun pada akun atau perangkat.
=== AKHIR PROFIL ===`

const profilNOC = `

=== PROFIL TIM: NOC ===
Kamu bekerja sebagai NOC (network operations center) ISP. Lawan bicaramu STAF INTERNAL.
- Singkat, teknis, berbasis bukti. Tanpa nada layanan pelanggan, tanpa basa-basi.
- Sebut angka dan status nyata dari hasil pengecekan. Tandai UNKNOWN bila sistem tidak
  tersedia; jangan menebak dan jangan menyatakan sistem sehat tanpa pengecekan.
- Kamu boleh melihat data lintas pelanggan sesuai jabatan staf yang terverifikasi.
- Tindakan yang mengubah jaringan atau data hanya sebagai rekomendasi; pelaksanaannya
  butuh persetujuan dan PIN sesuai kebijakan.
=== AKHIR PROFIL ===`

const profilCSLead = `

=== PROFIL TIM: CS LEAD (ADMIN) ===
Kamu melayani STAF ADMIN: urusan data pelanggan dan billing.
- Singkat dan faktual. Data billing lintas pelanggan boleh sesuai jabatan.
- Kamu tidak menangani tindakan jaringan; arahkan ke NOC bila perlu.
- Fakta harus dari hasil pengecekan; bila belum dicek, katakan belum diperiksa.
=== AKHIR PROFIL ===`

// profilFor mengembalikan blok profil untuk sebuah tim ("" bila tak dikenal).
func profilFor(t router.Team) string {
	switch t {
	case router.TeamCS:
		return profilCS
	case router.TeamNOC:
		return profilNOC
	case router.TeamCSLead:
		return profilCSLead
	}
	return ""
}

// teamPromptEnabled: profil hanya dipasang pada mode "on" untuk presenter.
// shadow/off -> prompt identik dengan perilaku sebelumnya.
func (e *Engine) teamPromptEnabled() bool {
	if e == nil || e.Cfg == nil {
		return false
	}
	return e.Cfg.Teams().Presenter == config.TeamOn
}

// teamProfileBlock memilih blok profil dari caller di context. Tanpa caller
// (pemanggilan operator/dashboard) -> kosong, perilaku lama.
func (e *Engine) teamProfileBlock(ctx context.Context) string {
	if !e.teamPromptEnabled() {
		return ""
	}
	c, ok := directory.CallerFrom(ctx)
	if !ok {
		return "" // tanpa caller (operator/dashboard): perilaku lama
	}
	return profilFor(router.TeamFor(c))
}
