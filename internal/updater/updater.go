// Package updater menyediakan deteksi versi dan pengecekan rilis terbaru dari
// GitHub Releases untuk mekanisme "cek otomatis + apply 1-klik".
//
// Desain sengaja PORTABEL (tidak hardcode satu server): owner/repo dikonfigurasi,
// pengecekan hanya READ ke GitHub API publik, dan keputusan apply diserahkan ke
// layer lain (endpoint + skrip installer). Package ini TIDAK menjalankan perintah
// sistem apa pun — murni versi + perbandingan semver + parse rilis — agar aman
// dan mudah diuji. Sesuai master spec: no silent prod change, apply butuh aksi
// operator eksplisit (ditangani di server, bukan di sini).
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Version yang disematkan saat build via -ldflags "-X ...updater.buildVersion=X".
// Kosong = dev build; CurrentVersion() lalu jatuh ke file VERSION bila ada.
var buildVersion = ""

// buildCommit & buildDate opsional, disematkan via ldflags untuk /api/version.
var (
	buildCommit = ""
	buildDate   = ""
)

// Info menggambarkan versi biner yang sedang berjalan.
type Info struct {
	Version string `json:"version"` // mis. "0.2.0" atau "dev"
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
}

// Release menggambarkan rilis GitHub yang relevan untuk update.
type Release struct {
	Tag         string    `json:"tag"`          // tag mentah, mis. "v0.2.0"
	Version     string    `json:"version"`      // tag tanpa prefix "v", mis. "0.2.0"
	Name        string    `json:"name"`         // judul rilis
	Notes       string    `json:"notes"`        // body/changelog
	URL         string    `json:"url"`          // halaman rilis (HTML)
	PublishedAt time.Time `json:"published_at"` // waktu publikasi
	Prerelease  bool      `json:"prerelease"`
}

// Status adalah hasil pengecekan update yang di-cache untuk dashboard.
type Status struct {
	Current         Info     `json:"current"`
	Latest          *Release `json:"latest,omitempty"`
	UpdateAvailable bool     `json:"update_available"`
	CheckedAt       time.Time `json:"checked_at"`
	Error           string   `json:"error,omitempty"`
}

// CurrentVersion mengembalikan versi biner: ldflags > file VERSION (di samping
// binary / project root) > "dev".
func CurrentVersion() Info {
	v := strings.TrimSpace(buildVersion)
	if v == "" {
		v = versionFromFile()
	}
	if v == "" {
		v = "dev"
	}
	return Info{Version: v, Commit: buildCommit, Date: buildDate}
}

// versionFromFile mencari file VERSION di samping executable lalu di CWD.
func versionFromFile() string {
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "VERSION"),
			filepath.Join(filepath.Dir(dir), "VERSION"), // bila binary di bin/
		)
	}
	candidates = append(candidates, "VERSION")
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				return s
			}
		}
	}
	return ""
}

// Checker menanyakan GitHub Releases dan meng-cache hasilnya. Aman untuk
// dipakai bersamaan (status terakhir dilindungi mutex).
type Checker struct {
	owner  string
	repo   string
	client *http.Client

	mu   sync.RWMutex
	last Status
}

// NewChecker membuat checker untuk owner/repo tertentu.
func NewChecker(owner, repo string) *Checker {
	return &Checker{
		owner:  owner,
		repo:   repo,
		client: &http.Client{Timeout: 10 * time.Second},
		last: Status{
			Current:   CurrentVersion(),
			CheckedAt: time.Time{},
		},
	}
}

// apiURL untuk rilis terbaru (dapat di-override di test).
func (c *Checker) apiURL() string {
	if v := strings.TrimSpace(os.Getenv("NOC_UPDATE_API_URL")); v != "" {
		return v
	}
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", c.owner, c.repo)
}

// Check mengambil rilis terbaru, membandingkan dengan versi saat ini, dan
// memperbarui cache. Error dikembalikan DAN disimpan di Status.Error agar
// dashboard bisa menampilkannya tanpa menebak.
func (c *Checker) Check(ctx context.Context) Status {
	cur := CurrentVersion()
	st := Status{Current: cur, CheckedAt: time.Now().UTC()}

	rel, err := c.fetchLatest(ctx)
	if err != nil {
		st.Error = err.Error()
		c.store(st)
		return st
	}
	st.Latest = rel
	st.UpdateAvailable = rel != nil && !rel.Prerelease && Compare(rel.Version, cur.Version) > 0
	c.store(st)
	return st
}

func (c *Checker) store(st Status) {
	c.mu.Lock()
	c.last = st
	c.mu.Unlock()
}

// Last mengembalikan status terakhir yang di-cache (tanpa memicu request baru).
func (c *Checker) Last() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st := c.last
	if st.CheckedAt.IsZero() {
		st.Current = CurrentVersion()
	}
	return st
}

// ghRelease adalah subset payload GitHub Releases API yang kita pakai.
type ghRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
}

func (c *Checker) fetchLatest(ctx context.Context) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "noc-sentinel-updater")
	// Token opsional menaikkan rate-limit GitHub (repo privat juga butuh ini).
	if tok := strings.TrimSpace(os.Getenv("NOC_UPDATE_GITHUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gagal menghubungi GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("belum ada rilis di repo (404)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub membalas HTTP %d", resp.StatusCode)
	}

	var gr ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return nil, fmt.Errorf("gagal baca respons rilis: %w", err)
	}
	return releaseFromGH(gr), nil
}

// releaseFromGH menormalkan payload GitHub ke Release.
func releaseFromGH(gr ghRelease) *Release {
	if gr.TagName == "" {
		return nil
	}
	rel := &Release{
		Tag:        gr.TagName,
		Version:    strings.TrimPrefix(gr.TagName, "v"),
		Name:       gr.Name,
		Notes:      gr.Body,
		URL:        gr.HTMLURL,
		Prerelease: gr.Prerelease || gr.Draft,
	}
	if gr.PublishedAt != "" {
		if t, err := time.Parse(time.RFC3339, gr.PublishedAt); err == nil {
			rel.PublishedAt = t
		}
	}
	return rel
}

// Compare membandingkan dua versi semver sederhana (mayor.minor.patch, prefix
// "v" diabaikan, label pra-rilis seperti "-rc1" diabaikan untuk urutan numerik).
// Mengembalikan 1 bila a>b, -1 bila a<b, 0 bila setara. Versi non-numerik
// (mis. "dev") dianggap paling lama (lebih kecil dari versi apa pun).
func Compare(a, b string) int {
	pa, oka := parseSemver(a)
	pb, okb := parseSemver(b)
	switch {
	case !oka && !okb:
		return 0
	case !oka: // a tak terparse (dev) < b
		return -1
	case !okb:
		return 1
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] > pb[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

// parseSemver mengurai "v1.2.3" / "1.2" / "1.2.3-rc1" menjadi [3]int.
func parseSemver(s string) ([3]int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return [3]int{}, false
	}
	// buang label pra-rilis / build metadata.
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	var out [3]int
	any := false
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
		any = true
	}
	return out, any
}
