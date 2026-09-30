// Package memory menyimpan apa yang sistem PELAJARI tentang tiap pengirim:
// fakta (lokasi, perangkat, catatan) dan riwayat insiden. Dipersist ke disk
// supaya tidak hilang saat aplikasi restart.
//
// Tujuan: saat pesan WhatsApp masuk, agen tidak mulai dari nol — ia sudah tahu
// "nomor ini pelanggan di Jl. Melati, pakai ONT Huawei, sudah 3x lapor lambat
// minggu ini".
package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Fact adalah satu hal yang diketahui tentang pengirim.
type Fact struct {
	Kind string    `json:"kind"` // lokasi | perangkat | catatan
	Text string    `json:"text"`
	At   time.Time `json:"at"`
	Hits int       `json:"hits"` // berapa kali disebut (semakin sering = semakin penting)
}

// Incident adalah ringkasan satu diagnosis untuk pengirim ini.
type Incident struct {
	ID         string    `json:"id"`
	At         time.Time `json:"at"`
	Query      string    `json:"query"`
	Signature  string    `json:"signature"` // kategori keluhan
	Intent     string    `json:"intent"`
	Verdict    string    `json:"verdict"`
	Confidence float64   `json:"confidence"`
	Target     string    `json:"target"`
	Probes     []string  `json:"probes"`
	Engine     string    `json:"engine"`
}

// Profile adalah ingatan lengkap satu pengirim.
type Profile struct {
	Key       string     `json:"key"`
	Facts     []Fact     `json:"facts"`
	Incidents []Incident `json:"incidents"`
	FirstSeen time.Time  `json:"first_seen"`
	LastSeen  time.Time  `json:"last_seen"`
}

// Store menyimpan semua profil. Aman dipakai bersamaan.
type Store struct {
	mu       sync.Mutex
	path     string
	profiles map[string]*Profile
	maxInc   int
	maxFacts int
	dirty    bool
}

type Config struct {
	Path     string // file JSON; kosong = memori saja (tidak disimpan)
	MaxInc   int    // maks insiden disimpan per pengirim
	MaxFacts int    // maks fakta per pengirim
}

func New(cfg Config) *Store {
	if cfg.MaxInc <= 0 {
		cfg.MaxInc = 50
	}
	if cfg.MaxFacts <= 0 {
		cfg.MaxFacts = 20
	}
	s := &Store{path: cfg.Path, profiles: map[string]*Profile{}, maxInc: cfg.MaxInc, maxFacts: cfg.MaxFacts}
	s.Load()
	return s
}

// ---- persistensi ----

func (s *Store) Load() error {
	if s.path == "" {
		return nil
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err // belum ada = wajar
	}
	var doc struct {
		Profiles map[string]*Profile `json:"profiles"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if doc.Profiles != nil {
		s.profiles = doc.Profiles
	}
	return nil
}

// Save menulis ke disk secara atomik (tulis file sementara lalu rename),
// supaya file tidak rusak bila proses mati di tengah penulisan.
func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	s.mu.Lock()
	doc := struct {
		Profiles map[string]*Profile `json:"profiles"`
		SavedAt  time.Time           `json:"saved_at"`
	}{Profiles: s.profiles, SavedAt: time.Now()}
	s.dirty = false
	s.mu.Unlock()

	b, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// SaveIfDirty dipanggil berkala oleh server; menulis hanya bila ada perubahan.
func (s *Store) SaveIfDirty() error {
	s.mu.Lock()
	d := s.dirty
	s.mu.Unlock()
	if !d {
		return nil
	}
	return s.Save()
}

// ---- tulis ----

func (s *Store) get(key string) *Profile {
	p, ok := s.profiles[key]
	if !ok {
		p = &Profile{Key: key, FirstSeen: time.Now()}
		s.profiles[key] = p
	}
	return p
}

// AddIncident mencatat satu diagnosis untuk pengirim ini.
func (s *Store) AddIncident(key string, inc Incident) {
	if key == "" || inc.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.get(key)
	p.Incidents = append(p.Incidents, inc)
	if len(p.Incidents) > s.maxInc {
		p.Incidents = p.Incidents[len(p.Incidents)-s.maxInc:]
	}
	p.LastSeen = time.Now()
	s.dirty = true
}

// AddFact menambahkan fakta. Bila fakta serupa sudah ada, hanya hitungannya
// yang bertambah — supaya "Jl. Melati" yang disebut 5x menjadi satu fakta kuat.
func (s *Store) AddFact(key, kind, text string) {
	text = strings.TrimSpace(text)
	if key == "" || text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.get(key)
	norm := strings.ToLower(text)
	for i := range p.Facts {
		if strings.EqualFold(p.Facts[i].Text, norm) || p.Facts[i].Text == text {
			p.Facts[i].Hits++
			p.Facts[i].At = time.Now()
			s.dirty = true
			return
		}
	}
	p.Facts = append(p.Facts, Fact{Kind: kind, Text: text, At: time.Now(), Hits: 1})
	if len(p.Facts) > s.maxFacts {
		// Buang fakta paling jarang disebut.
		sort.SliceStable(p.Facts, func(i, j int) bool { return p.Facts[i].Hits > p.Facts[j].Hits })
		p.Facts = p.Facts[:s.maxFacts]
	}
	p.LastSeen = time.Now()
	s.dirty = true
}

// ---- baca ----

// Facts mengembalikan fakta pengirim, diurutkan dari yang paling sering disebut.
func (s *Store) Facts(key string) []Fact {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[key]
	if !ok {
		return nil
	}
	out := append([]Fact(nil), p.Facts...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Hits > out[j].Hits })
	return out
}

// RecentIncidents mengembalikan insiden dalam rentang waktu tertentu (terbaru dulu).
func (s *Store) RecentIncidents(key string, since time.Duration) []Incident {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[key]
	if !ok {
		return nil
	}
	cut := time.Now().Add(-since)
	var out []Incident
	for i := len(p.Incidents) - 1; i >= 0; i-- {
		if p.Incidents[i].At.Before(cut) {
			continue
		}
		out = append(out, p.Incidents[i])
	}
	return out
}

// Recurrence merangkum pengulangan keluhan: berapa kali kategori keluhan yang
// sama muncul dalam rentang waktu, dan verdict terakhirnya.
func (s *Store) Recurrence(key, signature string, since time.Duration) (count int, lastVerdict string, lastAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[key]
	if !ok {
		return 0, "", time.Time{}
	}
	cut := time.Now().Add(-since)
	for i := len(p.Incidents) - 1; i >= 0; i-- {
		inc := p.Incidents[i]
		if inc.At.Before(cut) {
			continue
		}
		if signature != "" && inc.Signature != signature {
			continue
		}
		count++
		if lastAt.IsZero() {
			lastVerdict, lastAt = inc.Verdict, inc.At
		}
	}
	return count, lastVerdict, lastAt
}

// Profile mengembalikan salinan profil (untuk dashboard).
func (s *Store) Profile(key string) *Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[key]
	if !ok {
		return nil
	}
	cp := *p
	cp.Facts = append([]Fact(nil), p.Facts...)
	cp.Incidents = append([]Incident(nil), p.Incidents...)
	return &cp
}

// Keys mengembalikan semua nomor yang dikenal.
func (s *Store) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.profiles))
	for k := range s.profiles {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Store) Stats() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	inc, facts := 0, 0
	for _, p := range s.profiles {
		inc += len(p.Incidents)
		facts += len(p.Facts)
	}
	return map[string]any{
		"pengirim_dikenal": len(s.profiles),
		"total_insiden":    inc,
		"total_fakta":      facts,
		"tersimpan":        s.path != "",
	}
}

// ---- ekstraksi fakta otomatis ----

var (
	// Penanda lokasi khas alamat Indonesia.
	//
	// JANGAN memakai \b di akhir setelah singkatan berpola "jl\.?" — \b akan
	// memotong match tepat setelah "Jl" karena "." bukan karakter kata, sehingga
	// "Jl. Melati" hanya tertangkap sebagai "Jl". Pola ini mengambil kata kunci
	// lalu 0-4 kata berikutnya, dan berhenti pada tanda baca pemisah.
	reLokasi = regexp.MustCompile(`(?i)(jl\.?|jalan|gang|gg\.?|dusun|dsn\.?|desa|kecamatan|kec\.?|kelurahan|perumahan|komplek|blok\s?[a-z0-9]+|rt\s?\d+|rw\s?\d+)(\s+[a-z0-9'\-]+){0,4}`)
	// Penanda perangkat.
	perangkatKata = []string{
		"ont", "onu", "router", "modem", "cpe", "access point", "wifi", "wlan",
		"kabel", "lan", "switch", "hub", "pon", "splitter", "mikrotik", "tplink", "tp-link",
		"huawei", "zte", "fiberhome", "nokia",
	}
)

// ExtractFacts menarik fakta sederhana dari pesan. Sengaja berbasis aturan
// (bukan LLM) supaya deterministik, cepat, dan bisa diuji.
func ExtractFacts(msg string) []Fact {
	var out []Fact
	seen := map[string]bool{}

	if m := strings.TrimSpace(reLokasi.FindString(msg)); m != "" {
		// Rapikan: buang kata sambung yang ikut tertangkap di ujung.
		m = strings.TrimSpace(strings.Trim(m, ".,;:-"))
		if len(m) >= 4 && !seen[strings.ToLower(m)] {
			seen[strings.ToLower(m)] = true
			out = append(out, Fact{Kind: "lokasi", Text: m, At: time.Now()})
		}
	}
	low := " " + strings.ToLower(msg) + " "
	for _, d := range perangkatKata {
		if strings.Contains(low, " "+d) || strings.Contains(low, d+" ") {
			t := strings.TrimSpace(d)
			if t != "" && !seen[t] {
				seen[t] = true
				out = append(out, Fact{Kind: "perangkat", Text: t, At: time.Now()})
			}
		}
	}
	return out
}
