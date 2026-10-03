package dedupe

import (
	"sync/atomic"
	"time"
)

// Remote = penyimpanan bersama (Redis). SetNX true = key baru (bukan duplikat).
type Remote interface {
	SetNX(key string, ttl time.Duration) (bool, error)
}

// Hybrid memakai Remote bila tersedia dan SELALU jatuh ke Store lokal bila Remote
// gagal. Store lokal juga diperbarui setiap saat, jadi saat Redis mati dedupe
// tetap bekerja dari riwayat lokal. Mode:
//
//	shadow : keputusan tetap dari Store lokal; Remote hanya dibandingkan/dicatat.
//	on     : Remote menentukan bila sehat, lokal bila Remote gagal.
type Hybrid struct {
	Local  *Store
	Remote Remote
	TTL    time.Duration
	Mode   func() string // "off" | "shadow" | "on"

	RemoteErrors int64
	Mismatches   int64
}

func (h *Hybrid) mode() string {
	if h.Mode == nil {
		return "off"
	}
	return h.Mode()
}

// Seen: true = duplikat.
func (h *Hybrid) Seen(fp string) bool {
	local := h.Local.Seen(fp) // selalu dihitung & dicatat
	m := h.mode()
	if h.Remote == nil || m == "off" {
		return local
	}
	isNew, err := h.Remote.SetNX("noc:dedupe:"+fp, h.TTL)
	if err != nil {
		atomic.AddInt64(&h.RemoteErrors, 1)
		return local // fallback lokal
	}
	remoteDup := !isNew
	if remoteDup != local {
		atomic.AddInt64(&h.Mismatches, 1)
	}
	if m == "on" {
		return remoteDup
	}
	return local
}

// Stats untuk panel (tanpa kunci/isi).
func (h *Hybrid) Stats() map[string]int64 {
	return map[string]int64{"remote_errors": atomic.LoadInt64(&h.RemoteErrors), "mismatches": atomic.LoadInt64(&h.Mismatches)}
}
