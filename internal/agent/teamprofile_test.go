package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"ainoc/internal/config"
	"ainoc/internal/diag"
	"ainoc/internal/directory"
	"ainoc/internal/llm"
	"ainoc/internal/router"
)

// systemPromptSeen menjalankan Engine dengan LLM palsu yang menangkap prompt
// sistem yang benar-benar dikirim.
func systemPromptSeen(t *testing.T, presenter string, c *directory.Caller, query string) string {
	t.Helper()
	var mu sync.Mutex
	var system string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		_ = json.Unmarshal(b, &req)
		mu.Lock()
		for _, m := range req.Messages {
			if m.Role == "system" && system == "" {
				system = m.Content
			}
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"BALASAN: ok"}}]}`))
	}))
	defer srv.Close()
	cfg := config.Default()
	cfg.TeamPresenter = presenter
	eng := New(cfg, llm.New(srv.URL, "k", "m", 5), diag.New(5), nil, nil, nil, nil)
	ctx := context.Background()
	if c != nil {
		ctx = directory.WithCaller(ctx, *c)
	}
	eng.Run(ctx, "628111000111", query, "")
	mu.Lock()
	defer mu.Unlock()
	return system
}

func TestProfilTimDipilihDariIdentitas(t *testing.T) {
	cust := pelangganUji
	noc := nocUji
	adm := directory.Caller{Number: "628111444555", Role: directory.RoleAdmin, IsStaff: true, Name: "Admin Uji"}

	cases := []struct {
		nama   string
		c      *directory.Caller
		harus  string
		larang []string
	}{
		{"pelanggan", &cust, "PROFIL TIM: CUSTOMER SERVICE", []string{"PROFIL TIM: NOC", "CS LEAD"}},
		{"noc", &noc, "PROFIL TIM: NOC", []string{"CUSTOMER SERVICE", "CS LEAD"}},
		{"admin", &adm, "PROFIL TIM: CS LEAD", []string{"CUSTOMER SERVICE", "PROFIL TIM: NOC"}},
	}
	for _, c := range cases {
		p := systemPromptSeen(t, "on", c.c, "halo")
		if !strings.Contains(p, c.harus) {
			t.Errorf("[%s] prompt tak memuat %q", c.nama, c.harus)
		}
		for _, l := range c.larang {
			if strings.Contains(p, l) {
				t.Errorf("[%s] prompt memuat profil yang salah %q", c.nama, l)
			}
		}
		if !strings.Contains(p, "STANDAR KONTEKS") {
			t.Errorf("[%s] dokumen standar harus tetap ada", c.nama)
		}
	}
}

// off dan shadow: prompt IDENTIK dengan perilaku sebelumnya (tanpa profil).
func TestProfilTimTidakDipasangSaatOffAtauShadow(t *testing.T) {
	c := pelangganUji
	dasar := systemPromptSeen(t, "off", &c, "halo")
	if strings.Contains(dasar, "PROFIL TIM") {
		t.Fatal("mode off tidak boleh memasang profil")
	}
	for _, mode := range []string{"shadow", "", "ngawur"} {
		if got := systemPromptSeen(t, mode, &c, "halo"); got != dasar {
			t.Errorf("mode %q mengubah prompt (harus identik dengan off)", mode)
		}
	}
}

// Tanpa caller (operator/dashboard): tidak ada profil walau mode on.
func TestProfilTimTanpaCallerPerilakuLama(t *testing.T) {
	if p := systemPromptSeen(t, "on", nil, "halo"); strings.Contains(p, "PROFIL TIM") {
		t.Fatal("tanpa caller tidak boleh ada profil")
	}
}

// Profil CS tidak boleh memuat nama tool, URL, atau data internal, dan harus
// memuat larangan yang diwajibkan.
func TestProfilCSIsiAman(t *testing.T) {
	for _, bad := range []string{"billing.", "radius.", "mikrotik.", "genieacs.", "http", "172.", "token", "api_key"} {
		if strings.Contains(strings.ToLower(profilCS), bad) {
			t.Errorf("profil CS memuat %q", bad)
		}
	}
	for _, want := range []string{"MILIK pelanggan", "pelanggan lain", "staf", "waktu perbaikan"} {
		if !strings.Contains(profilCS, want) {
			t.Errorf("profil CS harus memuat %q", want)
		}
	}
}

func TestProfilForTimTakDikenal(t *testing.T) {
	if profilFor(router.Team("")) != "" || profilFor(router.Team("xyz")) != "" {
		t.Fatal("tim tak dikenal harus tanpa profil")
	}
}
