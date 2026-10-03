// Package handoff = buku serah-terima per kasus antara tim CS dan tim NOC
// (docs/PLAN_DUA_TIM_CS_NOC.md, Fase 4).
//
// Satu Handoff dibuka saat sebuah kasus dieskalasi (CS -> NOC). Staf membalas
// lewat Apply (NOC -> CS) dengan status dan, bila perlu, kalimat
// `untuk_pelanggan` — satu-satunya teks yang boleh sampai ke pelanggan.
//
// Paket ini tidak mengirim pesan dan tidak memanggil sistem lain: hanya aturan
// dan penyimpanan (JSON atomik). Pengiriman dilakukan pemanggil.
package handoff

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusOpen        Status = "open"
	StatusInvestigate Status = "diselidiki"
	StatusNeedField   Status = "butuh_lapangan"
	StatusNeedInfo    Status = "butuh_info"
	StatusClosed      Status = "selesai"
)

// Sistem yang wajib punya baris bukti; nilai UNKNOWN bila tak bisa diperiksa.
var Systems = []string{"billing", "radius", "mikrotik", "genieacs"}

const Unknown = "UNKNOWN"

var (
	ErrNotFound    = errors.New("kasus tidak ditemukan")
	ErrClosed      = errors.New("kasus sudah ditutup")
	ErrDuplicate   = errors.New("pembaruan identik dengan yang terakhir")
	ErrNeedMessage = errors.New("penutupan butuh pesan untuk pelanggan")
	ErrBadStatus   = errors.New("status tidak dikenal")
)

// Update = satu pembaruan dari staf. Untuk berisi kalimat untuk pelanggan.
type Update struct {
	At        time.Time `json:"at"`
	By        string    `json:"by"` // label internal (peran); tak pernah ke pelanggan
	Status    Status    `json:"status"`
	Untuk     string    `json:"untuk_pelanggan,omitempty"`
	Notified  bool      `json:"notified"`
	NotifyErr string    `json:"notify_err,omitempty"`
}

// Handoff = satu kasus yang diserahkan.
type Handoff struct {
	CaseID    string            `json:"case_id"`
	Customer  string            `json:"customer"` // nomor/identitas yang dikabari
	Domain    string            `json:"domain"`   // network | billing | ...
	Complaint string            `json:"complaint"`
	Evidence  map[string]string `json:"evidence"` // per sistem; UNKNOWN bila tak diketahui
	Severity  string            `json:"severity,omitempty"`
	Status    Status            `json:"status"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Updates   []Update          `json:"updates,omitempty"`
}

// Normalize memastikan bukti lengkap per sistem (tidak ada yang kosong).
func (h *Handoff) normalize() {
	if h.Evidence == nil {
		h.Evidence = map[string]string{}
	}
	for _, s := range Systems {
		if strings.TrimSpace(h.Evidence[s]) == "" {
			h.Evidence[s] = Unknown
		}
	}
	if h.Status == "" {
		h.Status = StatusOpen
	}
}

func (h Handoff) clone() Handoff {
	c := h
	c.Evidence = make(map[string]string, len(h.Evidence))
	for k, v := range h.Evidence {
		c.Evidence[k] = v
	}
	c.Updates = append([]Update(nil), h.Updates...)
	return c
}

// Ledger = penyimpanan handoff, aman untuk akses bersamaan.
type Ledger struct {
	mu   sync.Mutex
	path string
	m    map[string]*Handoff
	now  func() time.Time
}

// New membuka ledger. path kosong = hanya memori. File rusak/tak ada = kosong.
func New(path string) *Ledger {
	l := &Ledger{path: path, m: map[string]*Handoff{}, now: time.Now}
	if path == "" {
		return l
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return l
	}
	var list []*Handoff
	if json.Unmarshal(b, &list) != nil {
		return l
	}
	for _, h := range list {
		if h != nil && h.CaseID != "" {
			h.normalize()
			l.m[h.CaseID] = h
		}
	}
	return l
}

func (l *Ledger) persist() error {
	if l.path == "" {
		return nil
	}
	list := make([]*Handoff, 0, len(l.m))
	for _, h := range l.m {
		list = append(list, h)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// Open mencatat handoff baru. Idempoten per CaseID: kedua kali tidak menimpa
// dan mengembalikan false.
func (l *Ledger) Open(h Handoff) (bool, error) {
	if l == nil || strings.TrimSpace(h.CaseID) == "" {
		return false, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.m[h.CaseID]; ok {
		return false, nil
	}
	h.normalize()
	h.Status = StatusOpen
	h.CreatedAt = l.now().UTC()
	h.UpdatedAt = h.CreatedAt
	c := h.clone()
	l.m[h.CaseID] = &c
	return true, l.persist()
}

// Get mengembalikan salinan.
func (l *Ledger) Get(id string) (Handoff, bool) {
	if l == nil {
		return Handoff{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.m[strings.ToUpper(strings.TrimSpace(id))]
	if !ok {
		return Handoff{}, false
	}
	return h.clone(), true
}

// ListOpen = kasus yang belum ditutup, tertua dulu.
func (l *Ledger) ListOpen() []Handoff {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Handoff
	for _, h := range l.m {
		if h.Status != StatusClosed {
			out = append(out, h.clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// ValidStatus: status yang boleh diset staf lewat Apply.
func ValidStatus(s Status) bool {
	switch s {
	case StatusInvestigate, StatusNeedField, StatusNeedInfo, StatusClosed:
		return true
	}
	return false
}

// Apply menerapkan pembaruan staf. Aturan (ditegakkan di sini, bukan di prompt):
//   - kasus harus ada dan belum ditutup;
//   - penutupan wajib membawa pesan untuk pelanggan;
//   - pembaruan identik dengan yang terakhir ditolak (anti kirim ganda).
//
// Mengembalikan salinan kasus dan indeks Update yang baru.
func (l *Ledger) Apply(id, by string, st Status, untuk string) (Handoff, int, error) {
	if l == nil {
		return Handoff{}, -1, ErrNotFound
	}
	if !ValidStatus(st) {
		return Handoff{}, -1, ErrBadStatus
	}
	untuk = strings.TrimSpace(untuk)
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.m[strings.ToUpper(strings.TrimSpace(id))]
	if !ok {
		return Handoff{}, -1, ErrNotFound
	}
	if h.Status == StatusClosed {
		return h.clone(), -1, ErrClosed
	}
	if st == StatusClosed && untuk == "" {
		return h.clone(), -1, ErrNeedMessage
	}
	if n := len(h.Updates); n > 0 {
		last := h.Updates[n-1]
		if last.Status == st && last.Untuk == untuk {
			return h.clone(), -1, ErrDuplicate
		}
	}
	now := l.now().UTC()
	h.Updates = append(h.Updates, Update{At: now, By: by, Status: st, Untuk: untuk})
	h.Status = st
	h.UpdatedAt = now
	return h.clone(), len(h.Updates) - 1, l.persist()
}

// MarkNotified mencatat hasil kirim ke pelanggan untuk Update ke-idx.
func (l *Ledger) MarkNotified(id string, idx int, errMsg string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.m[strings.ToUpper(strings.TrimSpace(id))]
	if !ok || idx < 0 || idx >= len(h.Updates) {
		return ErrNotFound
	}
	h.Updates[idx].Notified = errMsg == ""
	h.Updates[idx].NotifyErr = errMsg
	return l.persist()
}
