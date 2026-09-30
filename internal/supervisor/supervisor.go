// Package supervisor menjalankan dan mengawasi WhatsApp Gateway (Node.js)
// sebagai child process dari binary Go ini.
//
// Tujuannya: satu perintah start untuk seluruh aplikasi. Go mengurus
// npm install sekali, menyalakan Node, memantau kesehatannya, dan
// me-restart otomatis bila mati. Gateway tetap berjalan sebagai proses
// terpisah karena protokol WhatsApp Web (Baileys) tidak mungkin
// direplikasi di Go tanpa menulis ulang ribuan baris.
package supervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type State string

const (
	StateDisabled   State = "disabled"   // folder gateway tidak ada
	StateInstalling State = "installing" // npm install sedang jalan
	StateStarting   State = "starting"
	StateRunning    State = "running" // proses hidup & /health menjawab
	StateUnhealthy  State = "unhealthy"
	StateStopped    State = "stopped"
	StateFailed     State = "failed"
)

type Status struct {
	State       State     `json:"state"`
	Dir         string    `json:"dir"`
	Port        int       `json:"port"`
	BaseURL     string    `json:"base_url"`
	PID         int       `json:"pid"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	Restarts    int       `json:"restarts"`
	LastError   string    `json:"last_error,omitempty"`
	NodeVersion string    `json:"node_version,omitempty"`
	Installed   bool      `json:"deps_installed"`
	Kredensial  bool      `json:"credentials_linked"`
	LogTail     []string  `json:"log_tail,omitempty"`
}

type Manager struct {
	Dir      string
	Port     int
	Env      []string
	LogLines int

	mu       sync.Mutex
	cmd      *exec.Cmd
	state    State
	restarts int
	lastErr  string
	started  time.Time
	logs     []string
	stopping bool
	cancel   context.CancelFunc
}

func New(dir string, port int, extraEnv []string, logLines int) *Manager {
	if port <= 0 {
		port = 3001
	}
	if logLines <= 0 {
		logLines = 80
	}
	return &Manager{Dir: dir, Port: port, Env: extraEnv, LogLines: logLines, state: StateStopped}
}

func (m *Manager) BaseURL() string { return fmt.Sprintf("http://127.0.0.1:%d", m.Port) }

// Present melaporkan apakah source gateway tersedia di disk.
func (m *Manager) Present() bool {
	if m.Dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(m.Dir, "app", "server.js"))
	return err == nil
}

func (m *Manager) depsInstalled() bool {
	_, err := os.Stat(filepath.Join(m.Dir, "node_modules", "@whiskeysockets"))
	return err == nil
}

// Status mengembalikan keadaan terkini; bila proses hidup tapi /health tidak
// menjawab, statusnya "unhealthy" (bukan running).
func (m *Manager) Status(ctx context.Context) Status {
	m.mu.Lock()
	st := Status{
		State:      m.state,
		Dir:        m.Dir,
		Port:       m.Port,
		BaseURL:    m.BaseURL(),
		Restarts:   m.restarts,
		LastError:  m.lastErr,
		StartedAt:  m.started,
		Installed:  m.depsInstalled(),
		Kredensial: m.KredensialLengkap(),
	}
	if m.cmd != nil && m.cmd.Process != nil {
		st.PID = m.cmd.Process.Pid
	}
	if len(m.logs) > 0 {
		tail := m.logs
		if len(tail) > 12 {
			tail = tail[len(tail)-12:]
		}
		st.LogTail = append([]string(nil), tail...)
	}
	m.mu.Unlock()

	if !m.Present() {
		st.State = StateDisabled
		return st
	}
	if v := nodeVersion(); v != "" {
		st.NodeVersion = v
	}
	// Proses hidup? Pastikan benar-benar melayani, bukan sekadar ada PID.
	if st.PID > 0 && healthOK(ctx, m.BaseURL()) {
		st.State = StateRunning
	} else if st.PID > 0 {
		st.State = StateUnhealthy
	} else if st.State == StateRunning || st.State == StateUnhealthy {
		st.State = StateStopped
	}
	return st
}

func nodeVersion() string {
	out, err := exec.Command("node", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func healthOK(ctx context.Context, base string) bool {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, base+"/api/whatsapp/health", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode < 500
}

func (m *Manager) log(line string) {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return
	}
	m.mu.Lock()
	m.logs = append(m.logs, line)
	if len(m.logs) > m.LogLines {
		m.logs = m.logs[len(m.logs)-m.LogLines:]
	}
	m.mu.Unlock()
}

func (m *Manager) setState(s State, err error) {
	m.mu.Lock()
	m.state = s
	if err != nil {
		m.lastErr = err.Error()
	}
	m.mu.Unlock()
}

// Start menjalankan gateway. Idempoten: aman dipanggil saat sudah berjalan.
// Otomatis menjalankan `npm install` bila node_modules belum ada.
func (m *Manager) Start(ctx context.Context) error {
	if !m.Present() {
		return fmt.Errorf("source gateway tidak ditemukan di %s", m.Dir)
	}
	m.mu.Lock()
	if m.cmd != nil && m.cmd.Process != nil {
		m.mu.Unlock()
		return nil // sudah jalan
	}
	m.stopping = false
	m.mu.Unlock()

	if !m.depsInstalled() {
		if err := m.npmInstall(ctx); err != nil {
			m.setState(StateFailed, err)
			return err
		}
	}

	if err := m.spawn(ctx); err != nil {
		m.setState(StateFailed, err)
		return err
	}
	go m.watch()
	return nil
}

// npmInstall menjalankan `npm install --omit=dev` sekali.
func (m *Manager) npmInstall(ctx context.Context) error {
	if _, err := exec.LookPath("npm"); err != nil {
		return fmt.Errorf("npm tidak ditemukan di PATH (install Node.js dulu)")
	}
	m.setState(StateInstalling, nil)
	m.log("$ npm install --omit=dev (sekali saja, ±30 detik)")
	c, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(c, "npm", "install", "--omit=dev", "--no-audit", "--no-fund")
	cmd.Dir = m.Dir
	if runtime.GOOS == "windows" {
		cmd.Env = append(os.Environ(), "npm_config_loglevel=error")
	}
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gagal menjalankan npm install: %w", err)
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		m.log("npm: " + sc.Text())
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("npm install gagal: %w", err)
	}
	if !m.depsInstalled() {
		return fmt.Errorf("npm install selesai tapi node_modules tidak lengkap")
	}
	m.log("[+] dependensi gateway siap")
	return nil
}

func (m *Manager) spawn(ctx context.Context) error {
	bin, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("node tidak ditemukan di PATH")
	}
	cmd := exec.Command(bin, "app/server.js")
	cmd.Dir = m.Dir
	env := append(os.Environ(),
		fmt.Sprintf("PORT=%d", m.Port),
		"AUTH_DIR="+filepath.Join(m.Dir, "data", "auth"),
	)
	// Arahkan webhook gateway ke aplikasi ini supaya loop WA->agen->WA utuh.
	env = append(env, "N8N_WEBHOOK_URL=http://127.0.0.1:8090/api/wa/webhook")
	env = append(env, m.Env...)
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gagal menjalankan node: %w", err)
	}
	m.mu.Lock()
	m.cmd = cmd
	m.state = StateStarting
	m.started = time.Now()
	m.mu.Unlock()

	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			m.log(sc.Text())
		}
	}()

	// Tunggu port siap (maks 20 detik).
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if healthOK(ctx, m.BaseURL()) {
			m.setState(StateRunning, nil)
			m.log(fmt.Sprintf("[+] gateway siap di %s (pid %d)", m.BaseURL(), cmd.Process.Pid))
			return nil
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return fmt.Errorf("proses node keluar sebelum siap")
		}
		time.Sleep(500 * time.Millisecond)
	}
	m.setState(StateUnhealthy, fmt.Errorf("gateway tidak menjawab dalam 20 detik"))
	return nil // tetap dianggap jalan; status menunjukkan unhealthy
}

// watch memantau proses dan me-restart bila mati tak sengaja.
func (m *Manager) watch() {
	m.mu.Lock()
	cmd := m.cmd
	m.mu.Unlock()
	if cmd == nil {
		return
	}
	err := cmd.Wait()

	m.mu.Lock()
	m.cmd = nil
	stopping := m.stopping
	m.mu.Unlock()

	if stopping {
		m.setState(StateStopped, nil)
		return
	}
	m.setState(StateStopped, err)
	m.log(fmt.Sprintf("[!] gateway berhenti (err=%v); restart otomatis dalam 5 detik", err))

	time.Sleep(5 * time.Second)
	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return
	}
	m.restarts++
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := m.spawn(ctx); err != nil {
		m.setState(StateFailed, err)
		return
	}
	go m.watch()
}

// Stop menghentikan gateway dengan rapi (SIGTERM, lalu kill bila perlu).
func (m *Manager) Stop() error {
	m.mu.Lock()
	m.stopping = true
	cmd := m.cmd
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		m.setState(StateStopped, nil)
		return nil
	}
	_ = cmd.Process.Signal(os.Interrupt)
	if runtime.GOOS == "windows" {
		// Windows tidak punya SIGINT untuk proses terpisah: pakai taskkill.
		_ = exec.Command("taskkill", "/PID", fmt.Sprint(cmd.Process.Pid), "/T", "/F").Run()
	}
	done := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		_ = cmd.Process.Kill()
	}
	m.mu.Lock()
	m.cmd = nil
	m.mu.Unlock()
	m.setState(StateStopped, nil)
	return nil
}

// Restart menghentikan lalu menyalakan ulang.
func (m *Manager) Restart(ctx context.Context) error {
	_ = m.Stop()
	time.Sleep(1 * time.Second)
	return m.Start(ctx)
}

// KredensialLengkap melaporkan apakah sesi WhatsApp sudah pernah ditautkan.
//
// PENTING: jangan memakai flag "registered" pada creds.json — pada Baileys 6.x
// flag itu tetap false walaupun sesi sudah CONNECTED dan nomor sudah terpasang.
// Penanda yang andal adalah adanya identitas akun di field "me".
func (m *Manager) KredensialLengkap() bool {
	if m.Dir == "" {
		return false
	}
	b, err := os.ReadFile(filepath.Join(m.Dir, "data", "auth", "creds.json"))
	if err != nil {
		return false
	}
	var doc struct {
		Me *struct {
			ID string `json:"id"`
		} `json:"me"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return false
	}
	return doc.Me != nil && strings.TrimSpace(doc.Me.ID) != ""
}

// StartAman menjalankan gateway dengan mencegah kehilangan sesi WhatsApp.
//
// Bila sesi sudah tertaut (creds.json registered=true) tetapi proses baru
// mendeteksi "stream errored / restart required", melanjutkan start berisiko
// meminta scan QR ulang. Maka start dibatalkan dan kredensial diamankan.
func (m *Manager) StartAman(ctx context.Context) (hasil string, err error) {
	if err := m.Start(ctx); err != nil {
		return "error", err
	}

	// Tunggu proses benar-benar menginisialisasi sesi.
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		m.mu.Lock()
		logs := strings.Join(m.logs, "\n")
		m.mu.Unlock()
		if strings.Contains(logs, "New QR Code generated") {
			break
		}
		if strings.Contains(logs, "Connected to WhatsApp") || strings.Contains(logs, "CONNECTED") {
			return "terhubung", nil
		}
	}

	m.mu.Lock()
	logs := strings.Join(m.logs, "\n")
	m.mu.Unlock()

	perluRestart := strings.Contains(logs, "Stream Errored") ||
		strings.Contains(logs, "restart required") ||
		strings.Contains(logs, "Connection Failure")
	mintaQR := strings.Contains(logs, "New QR Code generated")
	adaKredensial := m.KredensialLengkap()

	if mintaQR && adaKredensial && perluRestart {
		cadangan := filepath.Join(m.Dir, "data", "auth-backup-"+time.Now().Format("20060102-150405"))
		if err := os.Rename(filepath.Join(m.Dir, "data", "auth"), cadangan); err != nil {
			return "dibatalkan", fmt.Errorf(
				"sesi WhatsApp lama terdeteksi bermasalah dan gateway meminta scan QR baru. "+
					"Start dibatalkan agar sesi tidak hilang, tapi kredensial gagal diamankan: %v", err)
		}
		_ = m.Stop()
		return "dibatalkan", fmt.Errorf(
			"sesi WhatsApp lama terdeteksi bermasalah (\"restart required\") dan gateway meminta scan QR baru. "+
				"Start dibatalkan agar sesi Anda tidak hilang; kredensial lama diamankan ke %s. "+
				"Untuk menautkan ulang: mulai ulang aplikasi lalu scan QR baru", cadangan)
	}

	if mintaQR {
		return "minta-qr", nil
	}
	return "berjalan", nil
}

// PortFree memeriksa apakah port siap dipakai (untuk pesan error yang jelas).
func (m *Manager) PortFree() bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", m.Port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
