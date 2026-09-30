// Package session menyimpan konteks percakapan TERPISAH per pengirim (nomor
// WhatsApp), lengkap dengan cache jawaban.
//
// Dua tujuan:
//  1. Isolasi — konteks pelanggan A tidak boleh tercampur dengan pelanggan B.
//  2. Cache — keluhan identik dari nomor sama dalam jendela waktu singkat tidak
//     perlu di-diagnosis ulang (hemat token & waktu).
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"ainoc/internal/llm"
	"ainoc/internal/standard"
)

// Entry adalah satu pertukaran pesan dalam riwayat percakapan.
type Entry struct {
	Role     string    `json:"role"` // user | assistant
	Content  string    `json:"content"`
	At       time.Time `json:"at"`
	Intent   string    `json:"intent,omitempty"`
	Verdict  string    `json:"verdict,omitempty"`
	CacheHit bool      `json:"cache_hit,omitempty"`
}

// Conversation menyimpan konteks satu pengirim.
type Conversation struct {
	Key       string    `json:"key"`
	Entries   []Entry   `json:"entries"`
	LastSeen  time.Time `json:"last_seen"`
	LastTopic string    `json:"last_topic,omitempty"`
	// LastTarget adalah target probe terakhir, supaya pesan lanjutan seperti
	// "itu masih lambat" tidak perlu menyebut ulang alamatnya.
	LastTarget string `json:"last_target,omitempty"`
	// LastIntent dipakai untuk mendeteksi pergantian topik.
	LastIntent string `json:"last_intent,omitempty"`
}

// cacheEntry menyimpan jawaban lengkap untuk sebuah pertanyaan.
type cacheEntry struct {
	Answer    string
	Verdict   string
	Conf      float64
	Engine    string
	ElapsedMS int64
	Steps     []string // ringkasan langkah tool (nama tool) untuk laporan ulang
	At        time.Time
}

// Store adalah penyimpan sesi + cache. Aman dipakai bersamaan (goroutine-safe).
type Store struct {
	mu       sync.Mutex
	convs    map[string]*Conversation
	cache    map[string]cacheEntry
	maxTurns int
	ttl      time.Duration
	cacheTTL time.Duration
	maxConvs int
	hits     int
	misses   int
}

// Config mengatur batas memori & masa berlaku.
type Config struct {
	MaxTurns int           // jumlah pertukaran yang disimpan per percakapan
	TTL      time.Duration // percakapan dianggap selesai setelah idle selama ini
	CacheTTL time.Duration // umur cache jawaban
	MaxConvs int           // batas jumlah percakapan aktif (proteksi memori)
}

func DefaultConfig() Config {
	return Config{
		MaxTurns: 10,
		TTL:      2 * time.Hour,
		CacheTTL: 10 * time.Minute,
		MaxConvs: 500,
	}
}

func New(cfg Config) *Store {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 10
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 2 * time.Hour
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 10 * time.Minute
	}
	if cfg.MaxConvs <= 0 {
		cfg.MaxConvs = 500
	}
	return &Store{
		convs:    make(map[string]*Conversation),
		cache:    make(map[string]cacheEntry),
		maxTurns: cfg.MaxTurns,
		ttl:      cfg.TTL,
		cacheTTL: cfg.CacheTTL,
		maxConvs: cfg.MaxConvs,
	}
}

// Key menormalkan identitas pengirim menjadi kunci sesi yang stabil:
// "628123456789@s.whatsapp.net" -> "628123456789".
func Key(identity string) string {
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
		return "unknown"
	}
	return out
}

// History mengembalikan riwayat percakapan sebagai pesan LLM.
// Bila percakapan sudah kedaluwarsa (idle melebihi TTL), riwayat dianggap kosong
// sehingga topik lama tidak terbawa.
func (s *Store) History(key string) []llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.convs[key]
	if !ok {
		return nil
	}
	if time.Since(c.LastSeen) > s.ttl {
		delete(s.convs, key)
		return nil
	}
	out := make([]llm.Message, 0, len(c.Entries))
	for _, e := range c.Entries {
		out = append(out, llm.Message{Role: e.Role, Content: e.Content})
	}
	return out
}

// Meta mengembalikan info percakapan (target & intent terakhir) untuk konteks.
func (s *Store) Meta(key string) (lastTarget, lastIntent string, turns int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.convs[key]
	if !ok || time.Since(c.LastSeen) > s.ttl {
		return "", "", 0
	}
	return c.LastTarget, c.LastIntent, len(c.Entries)
}

// Append menambahkan satu pertukaran ke riwayat pengirim.
func (s *Store) Append(key, role, content, intent, verdict, target string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.convs[key]
	if !ok {
		s.evictIfNeeded()
		c = &Conversation{Key: key}
		s.convs[key] = c
	}
	c.Entries = append(c.Entries, Entry{
		Role: role, Content: content, At: time.Now(), Intent: intent, Verdict: verdict,
	})
	// Simpan hanya beberapa giliran terakhir (batas memori).
	if len(c.Entries) > s.maxTurns*2 {
		c.Entries = c.Entries[len(c.Entries)-s.maxTurns*2:]
	}
	c.LastSeen = time.Now()
	if intent != "" {
		c.LastIntent = intent
	}
	if target != "" {
		c.LastTarget = target
	}
}

// evictIfNeeded membuang percakapan kedaluwarsa, lalu yang paling lama idle
// bila masih melebihi batas. Dipanggil saat pemanggil sudah memegang lock.
func (s *Store) evictIfNeeded() {
	if len(s.convs) < s.maxConvs {
		// Bersihkan yang kedaluwarsa sesekali tanpa biaya besar.
		for k, c := range s.convs {
			if time.Since(c.LastSeen) > s.ttl {
				delete(s.convs, k)
			}
		}
		return
	}
	var oldestKey string
	var oldest time.Time
	for k, c := range s.convs {
		if oldestKey == "" || c.LastSeen.Before(oldest) {
			oldestKey, oldest = k, c.LastSeen
		}
	}
	if oldestKey != "" {
		delete(s.convs, oldestKey)
	}
}

// CacheKey membentuk kunci cache dari identitas + pertanyaan yang sudah
// dinormalkan. Normalisasi (huruf kecil, tanda baca dibuang) membuat
// "Internet Lambat!" dan "internet lambat" berbagi entri yang sama.
func CacheKey(identity, query string) string {
	h := sha256.Sum256([]byte(Key(identity) + "|" + standard.Normalize(query)))
	return hex.EncodeToString(h[:])[:24]
}

// CacheLookup mengambil jawaban tersimpan (bila masih berlaku).
// Hasil cache hanya dipakai untuk pertanyaan yang sama dari nomor yang sama,
// jadi konteks antar pelanggan tetap terisolasi.
func (s *Store) CacheLookup(key string) (Answer, Verdict string, Conf float64, Engine string, ElapsedMS int64, Steps []string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, found := s.cache[key]
	if !found || time.Since(e.At) > s.cacheTTL {
		if found {
			delete(s.cache, key)
		}
		s.misses++
		return "", "", 0, "", 0, nil, false
	}
	s.hits++
	return e.Answer, e.Verdict, e.Conf, e.Engine, e.ElapsedMS, e.Steps, true
}

// CacheStore menyimpan jawaban. Hanya jawaban yang layak (sudah ada verdict
// atau memang balasan tanpa pengecekan) yang boleh disimpan.
func (s *Store) CacheStore(key, answer, verdict string, conf float64, engine string, elapsedMS int64, steps []string) {
	if strings.TrimSpace(answer) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[key] = cacheEntry{
		Answer: answer, Verdict: verdict, Conf: conf, Engine: engine,
		ElapsedMS: elapsedMS, Steps: steps, At: time.Now(),
	}
	// Jaga ukuran cache.
	if len(s.cache) > s.maxConvs*4 {
		var oldestKey string
		var oldest time.Time
		for k, e := range s.cache {
			if oldestKey == "" || e.At.Before(oldest) {
				oldestKey, oldest = k, e.At
			}
		}
		delete(s.cache, oldestKey)
	}
}

// LastTarget mengembalikan target probe terakhir untuk pengirim ini.
func (s *Store) LastTarget(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.convs[key]
	if !ok || time.Since(c.LastSeen) > s.ttl {
		return ""
	}
	return c.LastTarget
}

// CacheTTL mengembalikan masa berlaku cache (untuk laporan/UI).
func (s *Store) CacheTTL() time.Duration { return s.cacheTTL }

// CacheInvalidate membuang cache satu pengirim. Dipakai saat jawaban model
// berubah (mis. model diganti) supaya tidak menyajikan hasil lama.
func (s *Store) CacheInvalidate(identity string) {
	prefix := Key(identity)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.cache {
		_ = k
	}
	// Cache tidak menyimpan identity mentah, jadi pembersihan menyeluruh lebih
	// aman dan murah dibanding menebak; dipakai hanya saat model berganti.
	_ = prefix
	s.cache = make(map[string]cacheEntry)
}

// CacheClearAll mengosongkan seluruh cache (dipakai saat model berganti).
func (s *Store) CacheClearAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = make(map[string]cacheEntry)
}

// Stats melaporkan kondisi sesi & cache untuk dashboard.
func (s *Store) Stats() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	active := 0
	for _, c := range s.convs {
		if time.Since(c.LastSeen) <= s.ttl {
			active++
		}
	}
	return map[string]any{
		"percakapan_aktif": active,
		"percakapan_total": len(s.convs),
		"cache_entri":      len(s.cache),
		"cache_hit":        s.hits,
		"cache_miss":       s.misses,
		"cache_ttl_detik":  int(s.cacheTTL.Seconds()),
		"sesi_ttl_detik":   int(s.ttl.Seconds()),
		"maks_giliran":     s.maxTurns,
	}
}

// Conversations mengembalikan ringkasan percakapan aktif (untuk dashboard).
func (s *Store) Conversations() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.convs))
	for _, c := range s.convs {
		if time.Since(c.LastSeen) > s.ttl {
			continue
		}
		last := ""
		if n := len(c.Entries); n > 0 {
			last = c.Entries[n-1].Content
			if len(last) > 100 {
				last = last[:100] + "…"
			}
		}
		out = append(out, map[string]any{
			"nomor":      c.Key,
			"giliran":    len(c.Entries),
			"terakhir":   last,
			"intent":     c.LastIntent,
			"target":     c.LastTarget,
			"idle_detik": int(time.Since(c.LastSeen).Seconds()),
		})
	}
	return out
}
