package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.1.9", 1},
		{"1.1.9", "1.2.0", -1},
		{"1.2.3", "1.2.3", 0},
		{"v1.2.3", "1.2.3", 0},
		{"2.0.0", "1.9.9", 1},
		{"1.2", "1.2.0", 0},
		{"1.2.1", "1.2", 1},
		{"1.2.3-rc1", "1.2.3", 0}, // label pra-rilis diabaikan untuk urutan numerik
		{"0.2.0", "0.1.0", 1},
		{"dev", "0.1.0", -1}, // non-semver dianggap paling lama
		{"0.1.0", "dev", 1},
		{"dev", "dev", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q)=%d, mau %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCurrentVersionFallbackDev(t *testing.T) {
	// Tanpa ldflags & tanpa file VERSION yang relevan di test dir, versi = "dev"
	// atau isi file VERSION bila kebetulan ada. Cukup pastikan tidak kosong.
	if CurrentVersion().Version == "" {
		t.Fatal("CurrentVersion() tidak boleh kosong")
	}
}

func TestCheckUpdateAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v99.0.0",
			"name": "Rilis Uji",
			"body": "catatan",
			"html_url": "https://example/releases/v99.0.0",
			"prerelease": false,
			"published_at": "2026-01-02T03:04:05Z"
		}`))
	}))
	defer srv.Close()

	t.Setenv("NOC_UPDATE_API_URL", srv.URL)
	c := NewChecker("owner", "repo")
	st := c.Check(context.Background())

	if st.Error != "" {
		t.Fatalf("tidak boleh error: %s", st.Error)
	}
	if st.Latest == nil || st.Latest.Version != "99.0.0" {
		t.Fatalf("latest salah: %+v", st.Latest)
	}
	if !st.UpdateAvailable {
		t.Errorf("update harus tersedia (99.0.0 > %s)", st.Current.Version)
	}
	// Last() harus mengembalikan status tersimpan.
	if c.Last().Latest == nil {
		t.Error("Last() tidak menyimpan status")
	}
}

func TestCheckNoUpdateWhenOlderRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.0.1","prerelease":false}`))
	}))
	defer srv.Close()

	t.Setenv("NOC_UPDATE_API_URL", srv.URL)
	// Paksa versi saat ini lebih baru dari rilis.
	old := buildVersion
	buildVersion = "1.0.0"
	defer func() { buildVersion = old }()

	c := NewChecker("owner", "repo")
	st := c.Check(context.Background())
	if st.UpdateAvailable {
		t.Errorf("tidak boleh ada update (current 1.0.0 > rilis 0.0.1)")
	}
}

func TestCheckPrereleaseIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v99.0.0","prerelease":true}`))
	}))
	defer srv.Close()

	t.Setenv("NOC_UPDATE_API_URL", srv.URL)
	c := NewChecker("owner", "repo")
	st := c.Check(context.Background())
	if st.UpdateAvailable {
		t.Errorf("pra-rilis tidak boleh memicu update tersedia")
	}
}

func TestCheckHandlesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("NOC_UPDATE_API_URL", srv.URL)
	c := NewChecker("owner", "repo")
	st := c.Check(context.Background())
	if st.Error == "" {
		t.Error("error HTTP 500 harus tercatat di Status.Error")
	}
	if st.UpdateAvailable {
		t.Error("tidak boleh klaim update saat error")
	}
}
