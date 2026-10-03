package caseengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func kasus(t *testing.T, tr *Tracker, ident string, steps ...State) *Case {
	t.Helper()
	c := tr.Replace(ident, "whatsapp")
	for _, s := range steps {
		if err := c.Transition(s, "uji", "uji"); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestPersistBertahanSetelahRestart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cases.json")
	a := NewTracker()
	a.SetPath(p)
	c1 := kasus(t, a, "628111000001", StateIdentifying, StateConversation, StateReadyForDiagnosis, StateEscalation)
	kasus(t, a, "628111000002", StateIdentifying) // lalu diganti -> arsip
	c2 := a.Replace("628111000002", "whatsapp")
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}

	b := NewTracker() // "proses baru"
	if err := b.Load(p); err != nil {
		t.Fatal(err)
	}
	if got, ok := b.Get("628111000001"); !ok || got.State() != StateEscalation || got.ID != c1.ID || got.Version() != c1.Version() {
		t.Fatalf("pemulihan salah: %+v ok=%v", got, ok)
	}
	if len(b.Get2("628111000001").Events()) != 4 {
		t.Fatal("riwayat event harus ikut pulih")
	}
	if got, _ := b.Get("628111000002"); got.ID != c2.ID {
		t.Fatal("case aktif terbaru harus pulih")
	}
	if len(b.All()) != 3 { // 2 aktif + 1 arsip
		t.Fatalf("All = %d, mau 3", len(b.All()))
	}
	// state machine tetap ditegakkan setelah pulih
	got, _ := b.Get("628111000001")
	if err := got.Transition(StateResolved, "uji", "lompat"); err == nil {
		t.Fatal("RESOLVED tanpa verifikasi harus tetap ditolak setelah pulih")
	}
	if err := got.Transition(StateHumanHandling, "uji", "ok"); err != nil {
		t.Fatalf("transisi sah harus jalan: %v", err)
	}
}

func (t *Tracker) Get2(id string) *Case { c, _ := t.Get(id); return c }

func TestLoadBerkasTakAdaBukanError(t *testing.T) {
	tr := NewTracker()
	if err := tr.Load(filepath.Join(t.TempDir(), "tidak-ada.json")); err != nil {
		t.Fatal(err)
	}
	if tr.Count() != 0 {
		t.Fatal("harus kosong")
	}
}

func TestLoadBerkasRusakDicadangkanTrackerUtuh(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cases.json")
	_ = os.WriteFile(p, []byte("{bukan json"), 0o600)
	tr := NewTracker()
	kasus(t, tr, "628111000009", StateIdentifying)
	if err := tr.Load(p); err == nil {
		t.Fatal("harus error")
	}
	if tr.Count() != 1 {
		t.Fatal("tracker tidak boleh berubah saat load gagal")
	}
	ent, _ := filepath.Glob(filepath.Join(d, "cases.json.rusak-*"))
	if len(ent) != 1 {
		t.Fatalf("berkas rusak harus dicadangkan: %v", ent)
	}
}

func TestLoadMenolakStateTakDikenalDanResolvedPalsu(t *testing.T) {
	for name, body := range map[string]string{
		"state asing":    `{"version":1,"active":[{"id":"CASE-1","identity":"628111000001","state":"HACKED"}]}`,
		"resolved palsu": `{"version":1,"active":[{"id":"CASE-2","identity":"628111000002","state":"RESOLVED"}]}`,
	} {
		p := filepath.Join(t.TempDir(), "c.json")
		_ = os.WriteFile(p, []byte(body), 0o600)
		tr := NewTracker()
		err := tr.Load(p)
		if err == nil {
			t.Errorf("%s: harus ditolak", name)
		} else if !strings.Contains(err.Error(), "case") && !strings.Contains(err.Error(), "state") {
			t.Errorf("%s: pesan: %v", name, err)
		}
		if tr.Count() != 0 {
			t.Errorf("%s: tracker harus tetap kosong", name)
		}
	}
}

func TestSaveTanpaPathNoop(t *testing.T) {
	tr := NewTracker()
	kasus(t, tr, "628111000001", StateIdentifying)
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveAtomikTanpaSisaTemp(t *testing.T) {
	d := t.TempDir()
	tr := NewTracker()
	tr.SetPath(filepath.Join(d, "x", "cases.json"))
	kasus(t, tr, "628111000001", StateIdentifying)
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "x", "cases.json.tmp")); err == nil {
		t.Fatal("temp tidak boleh tersisa")
	}
}
