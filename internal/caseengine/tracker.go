// Tracker adalah penyimpanan case in-memory fase 1: memetakan identity
// (nomor ternormalisasi) ke Case aktif. Belum ada persistence JSON/PostgreSQL —
// itu menyusul di fase berikutnya tanpa mengubah kontrak state machine di sini.
//
// Tracker hanya memetakan identity -> *Case. Keputusan lifecycle (state mana yang
// didorong, kapan reset) ada di pemanggil (internal/agent), supaya package ini
// tetap bebas dari pengetahuan tentang intent/diagnosis/HTTP.
package caseengine

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Tracker melacak case aktif per identity. Aman untuk dipakai dari banyak
// goroutine (mutex melindungi map; Case sendiri dipakai oleh satu goroutine
// per pesan — alur WhatsApp sudah mendedupe pesan per nomor).
type Tracker struct {
	mu    sync.RWMutex
	cases map[string]*Case
	now   func() time.Time
}

// NewTracker membuat tracker kosong dengan jam waktu nyata.
func NewTracker() *Tracker {
	return &Tracker{cases: make(map[string]*Case), now: time.Now}
}

// Get mengembalikan case untuk identity tanpa membuat yang baru.
func (t *Tracker) Get(identity string) (*Case, bool) {
	if t == nil {
		return nil, false
	}
	key := normalizeIdentity(identity)
	t.mu.RLock()
	defer t.mu.RUnlock()
	c, ok := t.cases[key]
	return c, ok
}

// Replace membuat case baru untuk identity dan menggantikan case lama (bila ada).
// Dipakai saat kontak baru dimulai setelah investigasi sebelumnya selesai.
func (t *Tracker) Replace(identity, channel string) *Case {
	key := normalizeIdentity(identity)
	now := time.Now()
	if t != nil && t.now != nil {
		now = t.now()
	}
	c := New(channel, key, now.UTC())
	if t == nil {
		return c
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cases[key] = c
	return c
}

// Put menyimpan case yang sudah ada (mis. dipulihkan dari persistence) ke
// tracker dengan kunci dari identity case. Berguna untuk observabilitas/restore
// tanpa menjalankan alur begin lagi. Mengembalikan case yang tersimpan.
func (t *Tracker) Put(c *Case) *Case {
	if t == nil || c == nil {
		return c
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cases[normalizeIdentity(c.Identity)] = c
	return c
}

// All mengembalikan salinan semua case (terbaru dulu) untuk observasi/audit.
func (t *Tracker) All() []*Case {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*Case, 0, len(t.cases))
	for _, c := range t.cases {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Count mengembalikan jumlah case yang sedang terlacak.
func (t *Tracker) Count() int {
	if t == nil {
		return 0
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.cases)
}

// normalizeIdentity menormalkan nomor pengirim menjadi kunci stabil:
// "628123456789@s.whatsapp.net" -> "628123456789". Sengaja disalin (bukan
// memakai internal/session) supaya package ini tetap tanpa dependensi eksternal.
func normalizeIdentity(identity string) string {
	s := strings.TrimSpace(identity)
	for _, suf := range []string{"@s.whatsapp.net", "@c.us", "@g.us", "@lid"} {
		if i := strings.Index(s, suf); i > 0 {
			s = s[:i]
		}
	}
	if i := strings.Index(s, ":"); i > 0 { // device id, mis. 628xxx:79
		s = s[:i]
	}
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		// Tanpa digit (mis. "unknown"): kembalikan identitas yang sudah
		// dibersihkan dari suffix/device, bukan identitas mentahnya.
		if s != "" {
			return s
		}
		return strings.TrimSpace(identity)
	}
	return out
}
