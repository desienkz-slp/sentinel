// notify.go — routing domain & penyusunan pesan handoff manusia (fase 2).
//
// Fase 1 menyiapkan deterministik routing (Target/Role) dan struktur payload;
// file ini menambahkan dua hal yang dibutuhkan untuk MENYAMPAIKAN handoff ke
// manusia melalui WhatsApp:
//
//  1. ClassifyDomain — menentukan domain (network vs billing) dari teks keluhan
//     pelanggan, supaya pemanggil tahu harus kirim ke NOC Senior atau Admin.
//  2. FormatHandoff — menyusun pesan lengkap berbahasa Indonesia (master spec
//     §17 & §45) tanpa membocorkan prompt internal / kredensial.
//  3. Dedup — penanda anti-spam: satu case hanya mengirim handoff satu kali.
//
// Package ini TIDAK melakukan pengiriman WA / audit — itu tanggung jawab
// pemanggil (server), sesuai batas fase 1 ("delivery/acknowledgement uses the
// durable outbox layer").
package escalation

import (
	"fmt"
	"strings"
	"sync"
)

// adminWords adalah sinyal keluhan administrasi/layanan yang tidak ambigu:
// transaksi, tagihan, perubahan paket/data, dsb. (master spec §4.2, §10
// NON-NETWORK). Dicocokkan SUBSTRING pada teks yang sudah di-lowercase.
var adminWords = []string{
	"tagihan", "billing", "pembayaran", "bayar", "membayar", "dibayar",
	"invoice", "refund", "kompensasi", "negosiasi", "berhenti berlangganan",
	"pendaftaran", "registrasi", "ubah paket", "ganti paket", "upgrade paket",
	"downgrade paket", "ubah data", "administrasi", "voucher", "saldo",
	"promo", "diskon", "kuota", "harga", "biaya", "faktur",
}

// networkWords adalah sinyal keluhan jaringan: gejala kuat + kosakata jaringan
// (master spec §10 NETWORK). Diperiksa SETELAH adminWords supaya keluhan yang
// memuat kata admin (mis. "tagihan ... error") tetap dirutekan ke Admin.
var networkWords = []string{
	"internet", "koneksi", "jaringan", "wifi", "wlan", "router", "modem",
	"ont", "onu", "cpe", "pppoe", "radius", "dns", "dhcp", "kabel", "fiber",
	"optik", "olt", "gpon", "sfp", "los", "lambat", "lemot", "lelet", "lamban",
	"putus", "terputus", "mati", "loss", "latency", "ping", "jitter",
	"buffering", "timeout", "rto", "offline", "disconnect", "tidak bisa",
	"gagal", "error", "eror", "isolir", "sinyal", "kecepatan", "bandwidth",
	"speedtest", "tidak terhubung", "tidak konek", "tidak jalan", "ngadat",
}

// ClassifyDomain menentukan domain eskalasi dari teks keluhan pelanggan.
//
// Keluhan administrasi/layanan -> "billing" (menuju Admin); keluhan jaringan ->
// "network" (menuju NOC Senior). Bila tidak ada sinyal yang jelas, default
// "network" — jalur yang sampai ke ESCALATION adalah keluhan gangguan, dan
// network adalah authority yang lebih teknis.
func ClassifyDomain(query string) string {
	n := strings.ToLower(strings.TrimSpace(query))
	if n == "" {
		return "network"
	}
	for _, w := range adminWords {
		if strings.Contains(n, w) {
			return "billing"
		}
	}
	for _, w := range networkWords {
		if strings.Contains(n, w) {
			return "network"
		}
	}
	return "network"
}

// FormatHandoff menyusun pesan WhatsApp handoff lengkap (Bahasa Indonesia) untuk
// manusia NOC Senior / Admin, menjawab 12 pertanyaan master spec §45. Nilai yang
// kosong ditampilkan "—" (tidak direka). Tidak memuat prompt internal / kredensial.
func FormatHandoff(c Context, role Role) string {
	roleLabel := "Admin"
	if role == RoleNOCSenior {
		roleLabel = "NOC Senior"
	}

	var b strings.Builder
	b.WriteString("🛰️ *NOC Sentinel — Eskalasi ke " + roleLabel + "*\n")

	field := func(label, val string) {
		v := strings.TrimSpace(val)
		if v == "" {
			v = "—"
		}
		fmt.Fprintf(&b, "*%s:* %s\n", label, v)
	}

	field("Case ID", c.CaseID)
	field("Domain", c.Domain)
	field("Pelanggan", c.Customer)
	field("Waktu mulai", c.Timeline)
	fmt.Fprintln(&b)
	field("Keluhan", c.Complaint)
	field("Ringkasan percakapan", c.ConversationSummary)

	// Status per sistem hanya relevan untuk authority jaringan (NOC Senior).
	if Target(c.Domain) == RoleNOCSenior {
		fmt.Fprintln(&b)
		field("Status Billing", c.BillingStatus)
		field("Status RADIUS", c.RadiusStatus)
		field("Status Router", c.RouterStatus)
		field("Status GenieACS", c.GenieACSStatus)
	}

	if len(c.Diagnostics) > 0 {
		fmt.Fprintln(&b)
		b.WriteString("*Diagnostik yang sudah dijalankan:*\n")
		for _, d := range c.Diagnostics {
			fmt.Fprintf(&b, "- %s\n", d)
		}
	}
	if len(c.Evidence) > 0 {
		fmt.Fprintln(&b)
		b.WriteString("*Bukti:*\n")
		for _, e := range c.Evidence {
			fmt.Fprintf(&b, "- %s\n", e)
		}
	}

	fmt.Fprintln(&b)
	field("Hipotesis", c.Hypothesis)
	field("Keyakinan", c.Confidence)

	if len(c.ActionsPerformed) > 0 {
		fmt.Fprintln(&b)
		b.WriteString("*Tindakan yang sudah dilakukan:*\n")
		for _, a := range c.ActionsPerformed {
			fmt.Fprintf(&b, "- %s\n", a)
		}
	}
	if len(c.ActionsNotPerformed) > 0 {
		fmt.Fprintln(&b)
		b.WriteString("*Tindakan yang belum dilakukan:*\n")
		for _, a := range c.ActionsNotPerformed {
			fmt.Fprintf(&b, "- %s\n", a)
		}
	}

	fmt.Fprintln(&b)
	field("Rekomendasi", c.Recommendation)
	field("Urgensi", c.Urgency)
	field("Cakupan terdampak", c.AffectedScope)
	field("Alasan eskalasi", c.Reason)
	field("Yang dibutuhkan dari Anda", c.HumanNeed)

	b.WriteString("\n—\n_Dikirim otomatis oleh NOC Sentinel L1._")
	return b.String()
}

// Dedup menandai satu case supaya handoff hanya dikirim satu kali (anti spam),
// meski alur yang sama berjalan berulang (webhook dobel, pesan lanjutan, dsb.).
type Dedup struct {
	mu   sync.Mutex
	sent map[string]bool
}

// NewDedup membuat penanda dedup kosong.
func NewDedup() *Dedup {
	return &Dedup{sent: make(map[string]bool)}
}

// Once mengembalikan true HANYA pada pemanggilan pertama untuk id tertentu,
// sekaligus menandainya sudah terkirim. Aman dipakai bersamaan (goroutine-safe).
func (d *Dedup) Once(id string) bool {
	if d == nil || strings.TrimSpace(id) == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sent == nil {
		d.sent = make(map[string]bool)
	}
	if d.sent[id] {
		return false
	}
	d.sent[id] = true
	return true
}
