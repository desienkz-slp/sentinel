package dedupe

import (
	"errors"
	"testing"
	"time"
)

type fakeRemote struct {
	seen map[string]bool
	fail bool
}

func (f *fakeRemote) SetNX(k string, _ time.Duration) (bool, error) {
	if f.fail {
		return false, errors.New("mati")
	}
	if f.seen[k] {
		return false, nil
	}
	f.seen[k] = true
	return true, nil
}

func hy(mode string, r *fakeRemote) *Hybrid {
	return &Hybrid{Local: New(time.Minute, 100), Remote: r, TTL: time.Minute, Mode: func() string { return mode }}
}

func TestHybridOffPerilakuLama(t *testing.T) {
	r := &fakeRemote{seen: map[string]bool{}}
	h := hy("off", r)
	if h.Seen("a") || !h.Seen("a") {
		t.Fatal("off = dedupe lokal biasa")
	}
	if len(r.seen) != 0 {
		t.Fatal("off tidak boleh menyentuh remote")
	}
}

func TestHybridOnRemoteMenentukan(t *testing.T) {
	r := &fakeRemote{seen: map[string]bool{"noc:dedupe:x": true}}
	h := hy("on", r)
	if !h.Seen("x") { // lokal belum lihat, remote sudah -> duplikat (mis. instance lain)
		t.Fatal("on: remote duplikat harus menang")
	}
}

func TestHybridShadowLokalMenentukanRemoteDicatat(t *testing.T) {
	r := &fakeRemote{seen: map[string]bool{"noc:dedupe:x": true}}
	h := hy("shadow", r)
	if h.Seen("x") {
		t.Fatal("shadow: keputusan tetap lokal (baru)")
	}
	if h.Stats()["mismatches"] != 1 {
		t.Fatalf("selisih harus tercatat: %v", h.Stats())
	}
}

// Redis MATI: dedupe tetap bekerja dari lokal, tidak ada pesan lolos ganda.
func TestHybridRemoteMatiJatuhKeLokal(t *testing.T) {
	r := &fakeRemote{seen: map[string]bool{}, fail: true}
	h := hy("on", r)
	if h.Seen("p") {
		t.Fatal("pertama bukan duplikat")
	}
	if !h.Seen("p") {
		t.Fatal("kedua HARUS duplikat walau Redis mati")
	}
	if h.Stats()["remote_errors"] < 2 {
		t.Fatalf("error remote harus tercatat: %v", h.Stats())
	}
}

func TestHybridTanpaRemote(t *testing.T) {
	h := &Hybrid{Local: New(time.Minute, 10), Mode: func() string { return "on" }}
	if h.Seen("q") || !h.Seen("q") {
		t.Fatal("tanpa remote = lokal")
	}
}
