// cs_unknown_wire.go — alur CS untuk penelepon yang TIDAK teridentifikasi di
// billing (nomor WA tidak cocok dengan pelanggan mana pun).
//
// Aturan operasional (keputusan operator):
//   1. Pelanggan tak dikenal yang MELAPORKAN KELUHAN -> CS wajib mencari tahu
//      nama lokasi (area/dusun/desa/nama WiFi) DULU, bukan langsung diagnosis.
//   2. Bila dari lokasi itu ditemukan pelanggan di billing -> lanjut diagnosis
//      normal dengan identitas pelanggan tersebut.
//   3. Bila TIDAK ditemukan -> eskalasi ke Admin (billing), bukan dibiarkan
//      "tidak ditemukan" tanpa tindak lanjut.
//
// Semua dijalankan KODE (bukan LLM) supaya perilaku sama untuk model apa pun,
// seperti halnya perintah staf (staff_command_wire.go).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"ainoc/internal/audit"
	"ainoc/internal/billing"
	"ainoc/internal/directory"
	"ainoc/internal/memory"
	"ainoc/internal/standard"
)

// csUnknownState melacak alur "tanya lokasi" per pengirim: menandai nomor yang
// sedang menunggu jawaban lokasi, supaya pesan lanjutan (jawaban lokasi) dibaca
// sebagai bagian dari alur yang sama, bukan keluhan baru.
type csUnknownState struct {
	mu      sync.Mutex
	waiting map[string]bool // session key -> sedang menunggu jawaban lokasi
}

func newCSUnknownState() *csUnknownState {
	return &csUnknownState{waiting: map[string]bool{}}
}

func (u *csUnknownState) isWaiting(key string) bool {
	if u == nil {
		return false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.waiting[key]
}

func (u *csUnknownState) markWaiting(key string) {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.waiting[key] = true
}

func (u *csUnknownState) clear(key string) {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.waiting, key)
}

// lokasiDariPesan mengekstrak nama lokasi dari pesan memakai ekstraksi fakta
// memory (aturan yang sama dipakai sistem untuk menyimpan lokasi pelanggan).
// Hanya pola lokasi eksplisit (Jl/Gang/Dusun/Desa/RT/RW/dll.) yang dianggap
// lokasi. Bila tidak ada, kembalikan "" — pemanggil memutuskan: minta lokasi
// (pesan pertama) ATAU pakai teks apa adanya (balasan setelah ditanya, yang
// bisa berupa nama WiFi/area tanpa penanda).
func lokasiDariPesan(msg string) string {
	for _, f := range memory.ExtractFacts(msg) {
		if f.Kind == "lokasi" && strings.TrimSpace(f.Text) != "" {
			return strings.TrimSpace(f.Text)
		}
	}
	return ""
}

// kueriDariBalasanLokasi mengambil teks singkat (maks 3 kata) dari balasan
// pelanggan yang sedang menjawab pertanyaan lokasi — bisa nama WiFi/area tanpa
// penanda lokasi. Kosong bila pesan kosong.
func kueriDariBalasanLokasi(msg string) string {
	fields := strings.Fields(strings.TrimSpace(msg))
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > 3 {
		fields = fields[:3]
	}
	return strings.Join(fields, " ")
}

// resolveUnknownCaller menangani satu pesan dari penelepon tak dikenal (bukan
// staf, bukan pelanggan billing). Mengembalikan:
//
//   - reply: teks balasan yang harus dikirim ke WhatsApp (bila handled).
//   - handled: true bila pesan ini ditangani penuh di sini (tanya lokasi /
//     eskalasi), sehingga alur LLM TIDAK perlu dijalankan.
//   - enriched: Caller pelanggan yang ter-resolve lewat lokasi (bila tepat satu
//     kecocokan di billing) — pemanggil melanjutkan diagnosis normal dengan
//     identitas ini.
//
// Bila pesan bukan keluhan nyata, handled=false dan alur normal berjalan.
func (s *Server) resolveUnknownCaller(ctx context.Context, caller directory.Caller, key, msg string) (reply string, handled bool, enriched *directory.Caller) {
	// Hanya penelepon yang benar-benar tak dikenal (bukan staf, bukan pelanggan).
	if caller.IsStaff || caller.IsCustomer {
		return "", false, nil
	}

	waiting := s.csUnknown.isWaiting(key)

	// Bila sender SEDANG menunggu pertanyaan lokasi, pesan apa pun dibaca sebagai
	// JAWABAN lokasi (bukan keluhan/sapaan baru) — jadi jangan diklasifikasi ulang.
	// Ini menutup bug: balasan "dusun krajan" terklasifikasi CHAT lalu jatuh ke LLM.
	if !waiting {
		// Hanya keluhan nyata yang memicu alur tanya-lokasi. Sapaan/info dari
		// nomor tak dikenal tetap ditangani alur LLM biasa (CS ramah).
		if standard.Classify(msg) != standard.IntentComplaint {
			return "", false, nil
		}
	}

	lokasi := lokasiDariPesan(msg)

	// Belum ada lokasi dan belum pernah ditanya -> minta lokasi.
	if lokasi == "" && !waiting {
		s.csUnknown.markWaiting(key)
		return "Mohon maaf, nomor ini belum terdaftar di data kami. " +
			"Agar saya bisa membantu, boleh sebutkan nama lokasi/area Anda " +
			"(mis. nama dusun, desa, gang, atau nama WiFi)?", true, nil
	}

	// Sedang menunggu jawaban lokasi: pakai lokasi eksplisit bila ada; kalau
	// tidak, pakai teks balasan apa adanya sebagai kueri (nama WiFi/area tanpa
	// penanda lokasi). Bila tetap kosong -> eskalasi ke Admin.
	if lokasi == "" {
		lokasi = kueriDariBalasanLokasi(msg)
	}
	if lokasi == "" {
		s.csUnknown.clear(key)
		s.escalateUnknownToAdmin(ctx, key, msg, "")
		return "Baik, terima kasih. Data Anda belum bisa kami temukan, " +
			"keluhan ini sudah saya teruskan ke tim Admin kami. " +
			"Mohon ditunggu, Admin akan menghubungi Anda.", true, nil
	}

	// Ada lokasi: cari di billing.
	s.csUnknown.clear(key)
	custs, err := s.searchBillingByLocation(ctx, lokasi)
	if err != nil {
		// Billing bermasalah -> jangan simpulkan "bukan pelanggan" (master spec
		// §52); eskalasi ke Admin supaya ada yang menindaklanjuti manual.
		s.escalateUnknownToAdmin(ctx, key, msg, lokasi)
		return "Mohon maaf, sistem kami sedang tidak bisa memeriksa data " +
			"pelanggan. Keluhan Anda sudah saya teruskan ke tim Admin, " +
			"mohon ditunggu ya.", true, nil
	}

	switch len(custs) {
	case 0:
		s.escalateUnknownToAdmin(ctx, key, msg, lokasi)
		return "Terima kasih atas informasinya. Data di lokasi tersebut belum " +
			"kami temukan, jadi keluhan Anda sudah saya teruskan ke tim Admin " +
			"untuk ditindaklanjuti. Mohon ditunggu ya.", true, nil
	case 1:
		c := custs[0]
		ci := &directory.CustomerInfo{
			Name:       c.Name,
			Username:   c.Username,
			Phone:      c.Phone,
			Status:     c.Status,
			IsIsolated: c.IsIsolated,
			Area:       c.Area.Name,
			Package:    c.Package.Name,
		}
		enriched = &directory.Caller{
			Number:     caller.Number,
			Role:       directory.RoleCustomer,
			Name:       c.Name,
			IsCustomer: true,
			Customer:   ci,
			Perms:      directory.PermsFor(directory.RoleCustomer),
		}
		log.Printf("[cs-unknown] penelepon %s di-resolve lewat lokasi %q -> %s (%s)", key, lokasi, c.Username, c.Name)
		return "", false, enriched
	default:
		// Banyak kecocokan: minta perjelas agar tidak salah mengaitkan pelanggan.
		s.csUnknown.markWaiting(key)
		return "Ditemukan beberapa pelanggan di lokasi tersebut. " +
			"Boleh diperjelas nama dusun/desa atau nama pelanggannya agar " +
			"saya bisa memastikan yang mana?", true, nil
	}
}

// searchBillingByLocation mencari pelanggan lewat kata kunci lokasi/nama. Bila
// adapter billing tidak terpasang, kembalikan error (bukan "tidak ditemukan").
func (s *Server) searchBillingByLocation(ctx context.Context, lokasi string) ([]billing.Customer, error) {
	if s.billing == nil {
		return nil, fmt.Errorf("billing tidak terkonfigurasi")
	}
	return s.billing.SearchByName(ctx, lokasi, 10)
}

// escalateUnknownToAdmin mengirim handoff pelanggan-tak-dikenal ke Admin
// (authority billing) lewat WhatsApp, dengan audit append-only.
func (s *Server) escalateUnknownToAdmin(ctx context.Context, identity, complaint, lokasi string) {
	to := s.escalationTarget("billing")
	if s.aud != nil {
		s.aud.Record(audit.Entry{
			EventType:  "cs_unknown_escalation",
			OccurredAt: time.Now().UTC(),
			Actor:      "cs",
			EntityType: "unknown_customer",
			EntityID:   identity,
			After: map[string]any{
				"lokasi":   lokasi,
				"keluhan":  clip(complaint, 200),
				"target":   to,
				"terkirim": false,
			},
			Note: "pelanggan tak dikenal dieskalasi ke Admin (billing)",
		})
	}
	if to == "" {
		log.Printf("[cs-unknown] eskalasi %s: nomor Admin belum dikonfigurasi — handoff tidak dikirim", identity)
		return
	}
	if s.wa == nil || !s.wa.Enabled() {
		log.Printf("[cs-unknown] eskalasi %s: gateway WA nonaktif — handoff tidak dikirim (tujuan %s)", identity, to)
		return
	}

	msg := "*🛰️ NOC Sentinel — Pelanggan belum terdaftar*\n\n" +
		"*Nomor WA:* " + identity + "\n" +
		"*Lokasi disebutkan:* " + orDashStr(lokasi) + "\n" +
		"*Keluhan:* " + clip(complaint, 300) + "\n\n" +
		"Pelanggan tidak ditemukan di billing. Mohon diverifikasi & ditindaklanjuti."

	sendCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.WATimeout)*time.Second)
	defer cancel()
	if _, err := s.wa.Send(sendCtx, to, msg); err != nil {
		log.Printf("[cs-unknown] GAGAL kirim eskalasi %s ke %s: %v", identity, to, err)
		if s.aud != nil {
			s.aud.Record(audit.Entry{EventType: "cs_unknown_escalation_sent", OccurredAt: time.Now().UTC(), Actor: "cs", EntityID: identity, ExecutionStatus: "FAILED", Error: err.Error()})
		}
		return
	}
	log.Printf("[cs-unknown] eskalasi %s terkirim ke Admin %s", identity, to)
	if s.aud != nil {
		s.aud.Record(audit.Entry{EventType: "cs_unknown_escalation_sent", OccurredAt: time.Now().UTC(), Actor: "cs", EntityID: identity, ExecutionStatus: "OK", After: map[string]any{"target": to}})
	}
}

func orDashStr(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
