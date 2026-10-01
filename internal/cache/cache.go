// Package cache menyediakan TTL cache generik thread-safe.
//
// Sesuai blueprint §9 (Cache Design) dan §65 (Cache Invalidation): cache
// berbeda dari memory LLM. Dipakai untuk data jangka pendek — status router,
// metadata pelanggan, hasil probe — dengan TTL eksplisit dan pembersihan
// otomatis entri kedaluwarsa.
//
// Cache ini TIDAK menjadi source of truth; selalu ada fallback ke sumber asli
// saat entry hilang/kedaluwarsa (cache miss).
package cache

import (
	"sync"
	"time"
)

// entry adalah satu nilai cache.
type entry struct {
	val     any
	expires time.Time
}

// Cache adalah TTL cache generik thread-safe.
type Cache struct {
	mu      sync.Mutex
	entries map[string]entry
	ttl     time.Duration
	onEvict func(key string, val any) // opsional, dipanggil saat evict manual
}

// New membuat cache dengan TTL default.
func New(ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &Cache{entries: map[string]entry{}, ttl: ttl}
}

// Set menyimpan nilai dengan TTL bawaan cache.
func (c *Cache) Set(key string, val any) {
	c.SetTTL(key, val, c.ttl)
}

// SetTTL menyimpan nilai dengan TTL khusus.
func (c *Cache) SetTTL(key string, val any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry{val: val, expires: time.Now().Add(ttl)}
}

// Get mengembalikan nilai bila masih valid; false bila hilang/kedaluwarsa.
func (c *Cache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expires) {
		delete(c.entries, key)
		return nil, false
	}
	return e.val, true
}

// GetOrSet mengembalikan nilai bila ada; bila tidak, panggil fn, simpan, dan
// kembalikan hasilnya (pola "cache-aside" yang aman dari race: fn dipanggil
// hanya sekali per key karena lock dipegang selama komputasi).
func (c *Cache) GetOrSet(key string, fn func() (any, error)) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok && time.Now().Before(e.expires) {
		return e.val, nil
	}
	v, err := fn()
	if err != nil {
		return nil, err
	}
	c.entries[key] = entry{val: v, expires: time.Now().Add(c.ttl)}
	return v, nil
}

// Delete menghapus satu key.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// Clear menghapus seluruh entri.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]entry{}
}

// Len menghitung entri yang BELUM kedaluwarsa.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	n := 0
	for k, e := range c.entries {
		if now.Before(e.expires) {
			n++
		} else {
			delete(c.entries, k)
		}
	}
	return n
}

// TTL mengembalikan TTL bawaan.
func (c *Cache) TTL() time.Duration { return c.ttl }
