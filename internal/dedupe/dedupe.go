// Package dedupe mencegah event duplikat membuat banyak insiden terpisah.
//
// Sesuai blueprint §30 (Event Deduplication) dan §31 (Incident Correlation):
// NOC sering menerima alert duplikat (webhook ganda, reconnect, pesan berulang).
// Setiap event punya fingerprint; dedupe menyaring yang sama dalam jendela
// waktu supaya satu insiden tidak dibuat berulang kali.
package dedupe

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Store adalah penyaring duplikat berbasis fingerprint + TTL.
type Store struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
	max  int
}

// New membuat store. ttl = berapa lama event yang sama dianggap duplikat.
func New(ttl time.Duration, max int) *Store {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if max <= 0 {
		max = 10000
	}
	return &Store{seen: map[string]time.Time{}, ttl: ttl, max: max}
}

// Fingerprint menghasilkan kunci deterministik dari bagian-bagian event.
// Elemen kosong diabaikan supaya event yang sama walau field opsional beda
// tetap menghasilkan fingerprint yang sama.
func Fingerprint(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		if p == "" {
			continue
		}
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Seen melaporkan apakah fingerprint ini sudah terlihat dalam jendela TTL.
// Bila baru pertama kali, mencatatnya dan mengembalikan false.
func (s *Store) Seen(fp string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.gc(now)

	if t, ok := s.seen[fp]; ok && now.Before(t.Add(s.ttl)) {
		return true // duplikat
	}
	s.seen[fp] = now
	if len(s.seen) > s.max {
		// Buang entri paling lama agar map tidak tumbuh tanpa batas.
		var oldest string
		var oldestT time.Time
		for k, v := range s.seen {
			if oldest == "" || v.Before(oldestT) {
				oldest, oldestT = k, v
			}
		}
		delete(s.seen, oldest)
	}
	return false
}

// Reset menghapus seluruh catatan.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = map[string]time.Time{}
}

// Len menghitung entri yang masih valid.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gc(time.Now())
	return len(s.seen)
}

// gc membuang entri kedaluwarsa (harus dipanggil dalam lock).
func (s *Store) gc(now time.Time) {
	for k, t := range s.seen {
		if now.After(t.Add(s.ttl)) {
			delete(s.seen, k)
		}
	}
}
