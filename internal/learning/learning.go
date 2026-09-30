// Package learning membuat sistem belajar dari hasil diagnosis sebelumnya.
//
// Dua hal yang dipelajari:
//  1. Signature keluhan -> probe mana yang terbukti paling berguna (playbook).
//  2. Pengulangan: keluhan sama berulang dalam rentang waktu -> tandai sebagai
//     masalah kronis, bukan gangguan sesaat.
//
// Semua pembelajaran berbasis STATISTIK dari hasil nyata, bukan tebakan model —
// jadi tetap berlaku walau model diganti.
package learning

import (
	"sort"
	"strings"
	"sync"
	"time"

	"ainoc/internal/standard"
)

// Signature adalah kategori keluhan yang dinormalkan, mis. "LAMBAT", "MATI_TOTAL".
type Signature string

const (
	SigLambat        Signature = "LAMBAT"
	SigMatiTotal     Signature = "MATI_TOTAL"
	SigDNS           Signature = "DNS"
	SigPPPoE         Signature = "PPPOE"
	SigWifi          Signature = "WIFI"
	SigPutusNyambung Signature = "PUTUS_NYAMBUNG"
	SigPerangkat     Signature = "PERANGKAT"
	SigUmum          Signature = "UMUM"
)

// signatureRules: urutan penting — yang lebih spesifik diperiksa lebih dulu.
var signatureRules = []struct {
	sig  Signature
	kata []string
}{
	{SigPPPoE, []string{"pppoe", "radius", "login gagal", "tidak bisa login", "isolir", "autentikasi"}},
	{SigDNS, []string{"dns", "tidak bisa buka", "gak bisa buka", "situs", "website", "domain", "tidak resolve"}},
	{SigWifi, []string{"wifi", "wlan", "sinyal", "jangkauan", "lemah"}},
	{SigPerangkat, []string{"ont", "onu", "modem", "router", "cpe", "perangkat", "kabel", "lan", "los", "optik"}},
	{SigPutusNyambung, []string{"putus nyambung", "kedip", "patah patah", "rto", "timeout", "hilang timbul", "naik turun"}},
	{SigMatiTotal, []string{"mati total", "mati", "tidak konek", "gak konek", "offline", "disconnect", "dc"}},
	{SigLambat, []string{"lambat", "lelet", "lemot", "lamban", "slow", "lag", "buffering", "loading terus", "loss", "ping tinggi", "latency"}},
}

// Signature menentukan kategori keluhan dari teks bebas.
func SignatureOf(msg string) Signature {
	n := standard.Normalize(msg)
	padded := " " + n + " "
	for _, r := range signatureRules {
		for _, k := range r.kata {
			if strings.Contains(padded, " "+k+" ") {
				return r.sig
			}
		}
	}
	return SigUmum
}

// ProbeStat mencatat keberhasilan sebuah probe untuk signature tertentu.
type ProbeStat struct {
	Probe   string `json:"probe"`
	Pakai   int    `json:"pakai"`   // berapa kali dijalankan
	Berguna int    `json:"berguna"` // berapa kali muncul pada diagnosis yang punya verdict pasti
	RataMS  int64  `json:"rata_ms"` // rata-rata durasi
}

// SigStat adalah statistik satu signature keluhan.
type SigStat struct {
	Signature Signature             `json:"signature"`
	Total     int                   `json:"total"`
	Verdict   map[string]int        `json:"verdict"` // distribusi verdict
	Probes    map[string]*ProbeStat `json:"probes"`
	Terakhir  time.Time             `json:"terakhir"`
	Konklusif int                   `json:"konklusif"` // jumlah diagnosis dgn verdict pasti
}

// Store menyimpan pembelajaran global.
type Store struct {
	mu   sync.Mutex
	sigs map[Signature]*SigStat
}

func New() *Store {
	return &Store{sigs: map[Signature]*SigStat{}}
}

// Record mencatat satu hasil diagnosis untuk dipelajari.
func (s *Store) Record(sig Signature, verdict string, probes []string, durationsMS map[string]int64) {
	if sig == "" {
		sig = SigUmum
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.sigs[sig]
	if !ok {
		st = &SigStat{Signature: sig, Verdict: map[string]int{}, Probes: map[string]*ProbeStat{}}
		s.sigs[sig] = st
	}
	st.Total++
	st.Terakhir = time.Now()
	if verdict != "" {
		st.Verdict[verdict]++
	}
	// Diagnosis dianggap konklusif bila verdictnya pasti (bukan TIDAK DIKETAHUI).
	konklusif := verdict == "SEHAT" || verdict == "DEGRADASI" || verdict == "GANGGUAN"
	if konklusif {
		st.Konklusif++
	}
	for _, p := range probes {
		ps, ok := st.Probes[p]
		if !ok {
			ps = &ProbeStat{Probe: p}
			st.Probes[p] = ps
		}
		ps.Pakai++
		if konklusif {
			ps.Berguna++
		}
		if d, ok := durationsMS[p]; ok {
			// rata-rata bergerak sederhana
			if ps.RataMS == 0 {
				ps.RataMS = d
			} else {
				ps.RataMS = (ps.RataMS*3 + d) / 4
			}
		}
	}
}

// Playbook mengembalikan urutan probe yang paling berguna untuk signature ini,
// berdasarkan hasil nyata. Bila data masih sedikit, kembalikan yang bawaan.
func (s *Store) Playbook(sig Signature) []string {
	s.mu.Lock()
	st, ok := s.sigs[sig]
	var stats []ProbeStat
	total := 0
	if ok {
		total = st.Total
		for _, ps := range st.Probes {
			stats = append(stats, *ps)
		}
	}
	s.mu.Unlock()

	if !ok || total < 3 || len(stats) == 0 {
		return Bawaan(sig)
	}
	// Urutkan: paling sering berguna, lalu paling cepat.
	sort.SliceStable(stats, func(i, j int) bool {
		ri := float64(stats[i].Berguna) / float64(max1(stats[i].Pakai))
		rj := float64(stats[j].Berguna) / float64(max1(stats[j].Pakai))
		if ri != rj {
			return ri > rj
		}
		return stats[i].RataMS < stats[j].RataMS
	})
	out := make([]string, 0, len(stats))
	for _, ps := range stats {
		out = append(out, ps.Probe)
	}
	return out
}

// Bawaan: playbook awal sebelum ada data nyata.
func Bawaan(sig Signature) []string {
	switch sig {
	case SigLambat:
		return []string{"ping", "http", "tcp"}
	case SigMatiTotal:
		return []string{"ping", "traceroute", "interface"}
	case SigDNS:
		return []string{"dns", "http", "tcp"}
	case SigPPPoE:
		return []string{"radius", "service", "ping"}
	case SigWifi:
		return []string{"interface", "ping"}
	case SigPutusNyambung:
		return []string{"ping", "traceroute"}
	case SigPerangkat:
		return []string{"interface", "ping", "service"}
	default:
		return []string{"ping", "dns", "tcp"}
	}
}

// DominanVerdict mengembalikan verdict yang paling sering muncul untuk signature
// ini ("" bila belum ada data cukup).
func (s *Store) DominanVerdict(sig Signature) (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.sigs[sig]
	if !ok || st.Konklusif < 3 {
		return "", 0
	}
	best, bestN := "", 0
	for v, n := range st.Verdict {
		if n > bestN {
			best, bestN = v, n
		}
	}
	return best, bestN
}

// Semua mengembalikan ringkasan pembelajaran (untuk dashboard).
func (s *Store) Semua() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.sigs))
	for _, st := range s.sigs {
		probes := make([]map[string]any, 0, len(st.Probes))
		for _, ps := range st.Probes {
			rasio := 0
			if ps.Pakai > 0 {
				rasio = 100 * ps.Berguna / ps.Pakai
			}
			probes = append(probes, map[string]any{
				"probe": ps.Probe, "pakai": ps.Pakai, "berguna": ps.Berguna,
				"rasio": rasio, "rata_ms": ps.RataMS,
			})
		}
		sort.SliceStable(probes, func(i, j int) bool {
			return probes[i]["rasio"].(int) > probes[j]["rasio"].(int)
		})
		out = append(out, map[string]any{
			"signature": st.Signature,
			"total":     st.Total,
			"konklusif": st.Konklusif,
			"verdict":   st.Verdict,
			"probes":    probes,
			"playbook":  s.PlaybookLocked(st.Signature),
			"terakhir":  st.Terakhir,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i]["total"].(int) > out[j]["total"].(int)
	})
	return out
}

// PlaybookLocked sama dengan Playbook tetapi mengasumsikan lock sudah dipegang.
func (s *Store) PlaybookLocked(sig Signature) []string {
	st, ok := s.sigs[sig]
	if !ok || st.Total < 3 || len(st.Probes) == 0 {
		return Bawaan(sig)
	}
	type kv struct {
		p string
		r float64
		m int64
	}
	var arr []kv
	for _, ps := range st.Probes {
		arr = append(arr, kv{ps.Probe, float64(ps.Berguna) / float64(max1(ps.Pakai)), ps.RataMS})
	}
	sort.SliceStable(arr, func(i, j int) bool {
		if arr[i].r != arr[j].r {
			return arr[i].r > arr[j].r
		}
		return arr[i].m < arr[j].m
	})
	out := make([]string, 0, len(arr))
	for _, a := range arr {
		out = append(out, a.p)
	}
	return out
}

// Stats untuk dashboard.
func (s *Store) Stats() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	total, konklusif := 0, 0
	for _, st := range s.sigs {
		total += st.Total
		konklusif += st.Konklusif
	}
	return map[string]any{
		"signature_dikenal": len(s.sigs),
		"total_diagnosis":   total,
		"konklusif":         konklusif,
	}
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
