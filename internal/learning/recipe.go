// recipe.go — "skill/cara pengecekan" yang ditemukan Endpoint B.
//
// Ketika Endpoint A (workflow deterministik) tidak konklusif, Endpoint B
// (reasoning) mencari cara pengecekan tambahan memakai tool READ yang lebih
// luas. Urutan tool yang TERBUKTI menghasilkan diagnosis pasti disimpan di sini
// sebagai "resep", supaya keluhan yang sama di kemudian hari langsung pakai
// urutan itu (lebih cepat & tidak mengulang pencarian).
//
// Resep dipersist ke disk (atomic) supaya bertahan restart/update. Ini BUKAN
// eksekusi kode baru — hanya urutan NAMA tool yang sudah terdaftar di registry
// dan tetap melewati gerbang registry -> policy -> adapter saat dipakai.
package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Recipe adalah satu cara pengecekan yang terbukti berhasil untuk satu
// signature keluhan. Tools = urutan nama tool READ yang dipakai (bukan kode).
type Recipe struct {
	Signature Signature `json:"signature"`
	Tools     []string  `json:"tools"`
	Verdict   string    `json:"verdict"` // verdict pasti yang dihasilkan (SEHAT/DEGRADASI/GANGGUAN)
	Hits      int       `json:"hits"`    // berapa kali resep ini terbukti berhasil
	LastUsed  time.Time `json:"last_used"`
	FirstSeen time.Time `json:"first_seen"`
}

// RecipeStore menyimpan resep per signature, dipersist ke disk.
type RecipeStore struct {
	mu    sync.Mutex
	path  string
	items map[Signature][]Recipe
	dirty bool
}

// NewRecipeStore membuat store resep. path kosong = in-memory only.
func NewRecipeStore(path string) *RecipeStore {
	s := &RecipeStore{path: path, items: map[Signature][]Recipe{}}
	s.Load()
	return s
}

// ---- persistensi (atomic: tulis tmp lalu rename) ----

func (s *RecipeStore) Load() {
	if s.path == "" {
		return
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return // belum ada = wajar
	}
	var doc struct {
		Recipes map[Signature][]Recipe `json:"recipes"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return
	}
	s.mu.Lock()
	if doc.Recipes != nil {
		s.items = doc.Recipes
	}
	s.mu.Unlock()
}

func (s *RecipeStore) Save() error {
	if s.path == "" {
		return nil
	}
	s.mu.Lock()
	doc := struct {
		Recipes map[Signature][]Recipe `json:"recipes"`
		SavedAt time.Time              `json:"saved_at"`
	}{Recipes: s.items, SavedAt: time.Now()}
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

func (s *RecipeStore) SaveIfDirty() error {
	s.mu.Lock()
	d := s.dirty
	s.mu.Unlock()
	if !d {
		return nil
	}
	return s.Save()
}

// ---- tulis ----

// Record menyimpan (atau memperkuat) satu resep yang terbukti menghasilkan
// verdict PASTI. Resep yang urutan tool-nya sudah ada hanya ditambah hit-nya.
func (s *RecipeStore) Record(sig Signature, tools []string, verdict string) {
	if sig == "" || len(tools) == 0 {
		return
	}
	// Hanya resep dengan verdict pasti yang layak dipelajari — "TIDAK DIKETAHUI"
	// bukan bukti keberhasilan (jangan belajar dari kegagalan).
	if verdict != "SEHAT" && verdict != "DEGRADASI" && verdict != "GANGGUAN" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.items[sig]
	key := strings.Join(tools, "\x00")
	now := time.Now()
	for i := range list {
		if strings.Join(list[i].Tools, "\x00") == key {
			list[i].Hits++
			list[i].LastUsed = now
			s.items[sig] = list
			s.dirty = true
			return
		}
	}
	list = append(list, Recipe{
		Signature: sig, Tools: append([]string(nil), tools...),
		Verdict: verdict, Hits: 1, LastUsed: now, FirstSeen: now,
	})
	s.items[sig] = list
	s.dirty = true
}

// Remove menghapus satu resep (signature + urutan tools) bila cocok.
func (s *RecipeStore) Remove(sig Signature, tools []string) error {
	key := strings.Join(tools, "\x00")
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.items[sig]
	for i, r := range list {
		if strings.Join(r.Tools, "\x00") == key {
			list = append(list[:i], list[i+1:]...)
			if len(list) == 0 {
				delete(s.items, sig)
			} else {
				s.items[sig] = list
			}
			s.dirty = true
			if err := s.Save(); err != nil {
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("resep tidak ditemukan")
}

// ---- baca ----

// BestFor mengembalikan urutan tool resep yang paling terbukti untuk signature
// ini (diurutkan: hits terbanyak, lalu paling baru dipakai). Kosong bila belum
// ada resep terpelajari — pemanggil memakai playbook statistik lama.
func (s *RecipeStore) BestFor(sig Signature) []string {
	s.mu.Lock()
	list := append([]Recipe(nil), s.items[sig]...)
	s.mu.Unlock()
	if len(list) == 0 {
		return nil
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Hits != list[j].Hits {
			return list[i].Hits > list[j].Hits
		}
		return list[i].LastUsed.After(list[j].LastUsed)
	})
	return append([]string(nil), list[0].Tools...)
}

// Semua mengembalikan ringkasan semua resep (untuk dashboard/observabilitas).
func (s *RecipeStore) Semua() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0)
	for sig, list := range s.items {
		for _, r := range list {
			out = append(out, map[string]any{
				"signature":  sig,
				"tools":      r.Tools,
				"verdict":    r.Verdict,
				"hits":       r.Hits,
				"last_used":  r.LastUsed,
				"first_seen": r.FirstSeen,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i]["hits"].(int) > out[j]["hits"].(int)
	})
	return out
}

func (s *RecipeStore) Stats() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, l := range s.items {
		n += len(l)
	}
	return map[string]any{"resep": n, "signature": len(s.items), "tersimpan": s.path != ""}
}
