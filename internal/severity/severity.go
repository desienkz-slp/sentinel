// Package severity menetapkan level P1..P4 secara deterministik (Fase 5,
// docs/PLAN_DUA_TIM_CS_NOC.md).
//
// Angka ambang BUKAN milik kode: operator yang mengisinya lewat konfigurasi.
// Tanpa ambang terisi, hasilnya UNRATED, bukan tebakan. Fungsi ini murni:
// tanpa I/O, tanpa LLM, tanpa jam.
package severity

import "strings"

// Level keparahan. UNRATED = kebijakan belum diisi operator.
type Level string

const (
	P1      Level = "P1"
	P2      Level = "P2"
	P3      Level = "P3"
	P4      Level = "P4"
	Unrated Level = "UNRATED"
)

// Rank: angka kecil = lebih gawat. UNRATED paling belakang.
func (l Level) Rank() int {
	switch l {
	case P1:
		return 1
	case P2:
		return 2
	case P3:
		return 3
	case P4:
		return 4
	}
	return 99
}

// Valid memeriksa nilai level yang dikenal (selain UNRATED).
func (l Level) Valid() bool { return l.Rank() <= 4 }

// ParseLevel menormalkan teks ("p1", " P2 ") ke Level; salah -> "" dan ok=false.
func ParseLevel(s string) (Level, bool) {
	l := Level(strings.ToUpper(strings.TrimSpace(s)))
	if l.Valid() {
		return l, true
	}
	return "", false
}

// Policy: seluruh angka diisi operator. Nol = ambang itu tidak dipakai.
type Policy struct {
	// Ambang jumlah pelanggan terdampak (>=). Harus P1 >= P2 >= P3 bila diisi.
	P1Customers int `json:"p1_customers"`
	P2Customers int `json:"p2_customers"`
	P3Customers int `json:"p3_customers"`
	// Cakupan yang otomatis dinaikkan ke level tertentu, mis. {"upstream":"P1"}.
	// Kunci: area, router, olt, pon, upstream.
	ScopeFloor map[string]string `json:"scope_floor,omitempty"`
	// Layanan kritis (mis. "radius") otomatis minimal level ini. Kosong = tak dipakai.
	CriticalFloor string `json:"critical_floor,omitempty"`
	// Floor: level bila bukti tidak ada (Affected<=0 dan tak ada cakupan).
	// Wajib diisi operator; kosong -> UNRATED.
	Floor string `json:"floor,omitempty"`
}

// Configured: kebijakan dianggap terisi bila ada ambang pelanggan, floor cakupan,
// atau floor kritis, DAN Floor valid.
func (p Policy) Configured() bool {
	if _, ok := ParseLevel(p.Floor); !ok {
		return false
	}
	return p.P1Customers > 0 || p.P2Customers > 0 || p.P3Customers > 0 ||
		len(p.ScopeFloor) > 0 || p.CriticalFloor != ""
}

// Validate mengembalikan alasan penolakan, "" bila sah.
func (p Policy) Validate() string {
	if p.P1Customers < 0 || p.P2Customers < 0 || p.P3Customers < 0 {
		return "ambang tidak boleh negatif"
	}
	if p.P1Customers > 0 && p.P2Customers > 0 && p.P1Customers < p.P2Customers {
		return "ambang P1 tidak boleh lebih kecil dari P2"
	}
	if p.P2Customers > 0 && p.P3Customers > 0 && p.P2Customers < p.P3Customers {
		return "ambang P2 tidak boleh lebih kecil dari P3"
	}
	if p.P1Customers > 0 && p.P3Customers > 0 && p.P1Customers < p.P3Customers {
		return "ambang P1 tidak boleh lebih kecil dari P3"
	}
	if p.Floor != "" {
		if _, ok := ParseLevel(p.Floor); !ok {
			return "floor harus P1..P4"
		}
	}
	if p.CriticalFloor != "" {
		if _, ok := ParseLevel(p.CriticalFloor); !ok {
			return "critical_floor harus P1..P4"
		}
	}
	for k, v := range p.ScopeFloor {
		switch k {
		case "area", "router", "olt", "pon", "upstream":
		default:
			return "scope_floor: kunci tidak dikenal: " + k
		}
		if _, ok := ParseLevel(v); !ok {
			return "scope_floor: level harus P1..P4"
		}
	}
	return ""
}

// Input: bukti yang SUDAH diverifikasi sistem. Field kosong = tidak diketahui.
type Input struct {
	Affected int      // pelanggan berbeda terdampak dalam jendela korelasi
	Scopes   []string // cakupan terverifikasi: area|router|olt|pon|upstream
	Critical bool     // melibatkan layanan kritis
}

// Result: level plus alasan yang bisa diaudit.
type Result struct {
	Level   Level
	Reasons []string
}

// Rate memilih level terburuk dari semua aturan yang terpenuhi.
func Rate(p Policy, in Input) Result {
	if !p.Configured() || p.Validate() != "" {
		return Result{Level: Unrated, Reasons: []string{"kebijakan keparahan belum diisi/valid"}}
	}
	best := Unrated
	var why []string
	raise := func(l Level, reason string) {
		why = append(why, reason)
		if l.Rank() < best.Rank() {
			best = l
		}
	}
	switch {
	case p.P1Customers > 0 && in.Affected >= p.P1Customers:
		raise(P1, "pelanggan terdampak >= ambang P1")
	case p.P2Customers > 0 && in.Affected >= p.P2Customers:
		raise(P2, "pelanggan terdampak >= ambang P2")
	case p.P3Customers > 0 && in.Affected >= p.P3Customers:
		raise(P3, "pelanggan terdampak >= ambang P3")
	}
	for _, sc := range in.Scopes {
		if v, ok := p.ScopeFloor[strings.ToLower(sc)]; ok {
			if l, ok := ParseLevel(v); ok {
				raise(l, "cakupan "+strings.ToLower(sc))
			}
		}
	}
	if in.Critical && p.CriticalFloor != "" {
		if l, ok := ParseLevel(p.CriticalFloor); ok {
			raise(l, "layanan kritis")
		}
	}
	if best == Unrated {
		l, _ := ParseLevel(p.Floor)
		return Result{Level: l, Reasons: []string{"tanpa bukti yang memenuhi ambang: level floor"}}
	}
	return Result{Level: best, Reasons: why}
}
