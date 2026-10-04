// Package mikrotik mengimplementasikan adapter read-only ke router MikroTik
// lewat API NATIVE RouterOS (protokol biner), BUKAN REST /www.
//
// Kontrak (lihat docs/mikrotik-api.md):
//   - Transport : TCP, protokol biner proprietary ("sentence" = deret word
//     ber-prefix panjang, ditutup byte 0x00).
//   - Port      : 8728 (plaintext) / 8729 (TLS / API-SSL) / custom.
//   - Auth      : /login =name=<user> =password=<pass> (plaintext, 6.43+)
//     atau challenge-response MD5 (pra-6.43, =ret=).
//
// Adapter ini HANYA read-only. Tindakan WRITE (mis. disconnect PPPoE) tetap
// lewat gerbang policy APPROVAL_REQUIRED dan dibangun terpisah.
package mikrotik

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"ainoc/internal/tool"
)

// Config adalah parameter koneksi API native RouterOS.
type Config struct {
	Name string // nama router (untuk multi-router); kosong = pakai Host
	Host string // alamat IP / hostname, mis. 192.168.171.1
	Port int    // 0 = default: 8728 (plaintext) / 8729 (TLS)
	User string
	Pass string
	TLS  bool // true = API-SSL (port 8729), false = plaintext (8728)
}

// Adapter adalah adaptor MikroTik read-only (API native RouterOS).
type Adapter struct {
	cfg     Config
	timeout time.Duration
}

// New membuat MikrotikAdapter. Host+user wajib; port 0 = default per TLS.
func New(c Config) *Adapter {
	return &Adapter{cfg: c, timeout: 8 * time.Second}
}

// Domain memenuhi tool.Adapter.
func (a *Adapter) Domain() string { return "mikrotik" }

// Name memenuhi tool.Adapter.
func (a *Adapter) Name() string {
	if a.cfg.Name != "" {
		return a.cfg.Name + " (MikroTik)"
	}
	return "MikroTik RouterOS (API native)"
}

// RouterName mengembalikan nama router (untuk list multi-router).
func (a *Adapter) RouterName() string {
	if a.cfg.Name != "" {
		return a.cfg.Name
	}
	return a.cfg.Host
}

// Host mengembalikan host router.
func (a *Adapter) Host() string { return a.cfg.Host }

// Configured memenuhi tool.Adapter.
func (a *Adapter) Configured() bool { return a.cfg.Host != "" && a.cfg.User != "" }

// ToolNames memenuhi tool.Adapter — hanya tool read-only.
func (a *Adapter) ToolNames() []string {
	return []string{
		"mikrotik.get_pppoe_status",
		"mikrotik.get_interface_stats",
		"mikrotik.get_interface_live",
		"mikrotik.get_customer_traffic",
	}
}

// port mengembalikan port koneksi: eksplisit > default (8728/8729).
func (a *Adapter) port() int {
	if a.cfg.Port != 0 {
		return a.cfg.Port
	}
	if a.cfg.TLS {
		return 8729
	}
	return 8728
}

// addr mengembalikan host:port untuk tampilan status.
func (a *Adapter) addr() string {
	return fmt.Sprintf("%s:%d", a.cfg.Host, a.port())
}

// Health memenuhi tool.Adapter: probe /system/resource (identitas router).
func (a *Adapter) Health(ctx context.Context) (string, error) {
	d, err := a.Ping(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("mikrotik menjawab (v%s, uptime %s)", d.Version, d.Uptime), nil
}

// PingResult adalah hasil verifikasi koneksi MikroTik untuk dashboard.
type PingResult struct {
	BaseURL   string `json:"base_url"`
	LatencyMS int64  `json:"ping_ms"`
	Version   string `json:"version"`
	BoardName string `json:"board_name"`
	Uptime    string `json:"uptime"`
	CPU       string `json:"cpu_load"`
	Identity  string `json:"identity"` // nama router di /system/identity
}

// Ping memverifikasi koneksi + auth terhadap RouterOS (tanpa membocorkan pass).
func (a *Adapter) Ping(ctx context.Context) (PingResult, error) {
	if !a.Configured() {
		return PingResult{}, fmt.Errorf("mikrotik belum dikonfigurasi (isi host + username + password)")
	}
	start := time.Now()
	rows, err := a.query(ctx, "/system/resource/print", nil)
	if err != nil {
		return PingResult{}, err
	}
	r := firstRow(rows)
	res := PingResult{
		BaseURL:   a.addr(),
		LatencyMS: time.Since(start).Milliseconds(),
		Version:   r["version"],
		BoardName: r["board-name"],
		Uptime:    r["uptime"],
		CPU:       r["cpu-load"],
	}
	// Identitas router (nama di /system/identity) — opsional, tidak gagal bila kosong.
	if idRows, err := a.query(ctx, "/system/identity/print", nil); err == nil {
		res.Identity = firstRow(idRows)["name"]
	}
	return res, nil
}

// InterfaceRunning reads only the requested RouterOS interface rows. It is
// deliberately not exposed through the tool dispatcher: the poll producer uses
// this narrow internal read-only seam and never discovers arbitrary interfaces.
func (a *Adapter) InterfaceRunning(ctx context.Context, names []string) (map[string]bool, error) {
	if !a.Configured() {
		return nil, fmt.Errorf("mikrotik belum dikonfigurasi (isi host + username + password)")
	}
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			wanted[name] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return map[string]bool{}, nil
	}
	out := make(map[string]bool, len(wanted))
	for name := range wanted {
		// RouterOS query operator restricts each read to the configured
		// interface; unconfigured interfaces are neither interpreted nor emitted.
		rows, err := a.query(ctx, "/interface/print", map[string]string{"?name": name})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row["name"] == name {
				out[name] = row["running"] == "true" && row["disabled"] != "true"
			}
		}
	}
	return out, nil
}

// Invoke memenuhi tool.Adapter.
func (a *Adapter) Invoke(ctx context.Context, name string, args map[string]any) (tool.Output, error) {
	switch name {
	case "mikrotik.get_pppoe_status":
		return a.getPPPoEStatus(ctx, args)
	case "mikrotik.get_interface_stats":
		return a.getInterfaceStats(ctx, args)
	case "mikrotik.get_interface_live":
		return a.getInterfaceLive(ctx, args)
	case "mikrotik.get_customer_traffic":
		return a.getCustomerTraffic(ctx, args)
	default:
		return tool.Output{}, fmt.Errorf("tool mikrotik tidak dikenal: %s", name)
	}
}

// getPPPoEStatus reads at most one PPPoE session. The RouterOS query is
// server-side bounded by the exact requested identity; bulk-session reads and
// prefix matching are deliberately forbidden.
func (a *Adapter) getPPPoEStatus(ctx context.Context, args map[string]any) (tool.Output, error) {
	identity := firstString(args, "identity")
	if identity == "" {
		return tool.Output{}, fmt.Errorf("mikrotik.get_pppoe_status butuh identity (username PPPoE tepat)")
	}

	rows, err := a.query(ctx, "/ppp/active/print", map[string]string{"?name": identity})
	if err != nil {
		return tool.Output{}, err
	}
	if len(rows) == 0 {
		return tool.Output{Text: fmt.Sprintf("PPPoE %q TIDAK aktif (tidak ada sesi di /ppp/active).", identity)}, nil
	}
	if len(rows) != 1 || rows[0]["name"] != identity {
		return tool.Output{Text: fmt.Sprintf("PPPoE %q tidak dapat di-resolve secara unik.", identity)}, nil
	}

	s := rows[0]
	var b strings.Builder
	fmt.Fprintf(&b, "Sesi PPPoE aktif untuk %q: service=%s", identity, orDash(s["service"]))
	if s["caller-id"] != "" {
		fmt.Fprintf(&b, ", caller-id=%s", s["caller-id"])
	}
	if s["address"] != "" {
		fmt.Fprintf(&b, ", ip=%s", s["address"])
	}
	if s["uptime"] != "" {
		fmt.Fprintf(&b, ", uptime=%s", s["uptime"])
	}
	return tool.Output{Data: []map[string]string{s}, Text: b.String()}, nil
}

// getInterfaceStats reads cumulative counters for one exact interface only.
// Field terkonfirmasi: rx-byte, tx-byte, rx-drop, rx-error.
func (a *Adapter) getInterfaceStats(ctx context.Context, args map[string]any) (tool.Output, error) {
	iface := firstString(args, "interface")
	if iface == "" {
		return tool.Output{}, fmt.Errorf("mikrotik.get_interface_stats butuh interface (nama interface tepat)")
	}
	rows, err := a.query(ctx, "/interface/print", map[string]string{"?name": iface})
	if err != nil {
		return tool.Output{}, err
	}
	if len(rows) == 0 {
		return tool.Output{Text: fmt.Sprintf("Interface %q tidak ditemukan atau tidak aktif.", iface)}, nil
	}
	if len(rows) != 1 || rows[0]["name"] != iface {
		return tool.Output{Text: fmt.Sprintf("Interface %q tidak dapat di-resolve secara unik.", iface)}, nil
	}
	s := rows[0]
	var b strings.Builder
	if s["disabled"] == "true" || s["running"] == "false" {
		return tool.Output{Text: fmt.Sprintf("Interface %q tidak aktif.", iface)}, nil
	}
	fmt.Fprintf(&b, "%s: rx=%s (%s), tx=%s (%s)",
		s["name"], s["rx-byte"], humanBytes(parseUint(s["rx-byte"])),
		s["tx-byte"], humanBytes(parseUint(s["tx-byte"])))
	if s["rx-drop"] != "" && s["rx-drop"] != "0" {
		fmt.Fprintf(&b, ", rx-drop=%s", s["rx-drop"])
	}
	if s["rx-error"] != "" && s["rx-error"] != "0" {
		fmt.Fprintf(&b, ", rx-error=%s", s["rx-error"])
	}
	if s["comment"] != "" {
		fmt.Fprintf(&b, " [%s]", s["comment"])
	}
	return tool.Output{Data: []map[string]string{s}, Text: b.String()}, nil
}

// getInterfaceLive membaca live bps via /interface/monitor-traffic once.
// Field terkonfirmasi: rx-bits-per-second, tx-bits-per-second, drops.
func (a *Adapter) getInterfaceLive(ctx context.Context, args map[string]any) (tool.Output, error) {
	iface := firstString(args, "interface")
	if iface == "" {
		return tool.Output{}, fmt.Errorf("mikrotik.get_interface_live butuh interface (nama interface)")
	}
	rows, err := a.query(ctx, "/interface/monitor-traffic", map[string]string{
		"interface": iface,
		"once":      "",
	})
	if err != nil {
		return tool.Output{}, err
	}
	if len(rows) == 0 {
		return tool.Output{Text: fmt.Sprintf("Tidak ada data traffic untuk %q.", iface)}, nil
	}
	if len(rows) != 1 || rows[0]["name"] != iface {
		return tool.Output{Text: fmt.Sprintf("Interface %q tidak dapat di-resolve secara unik.", iface)}, nil
	}
	s := rows[0]
	rx := parseUint(s["rx-bits-per-second"])
	tx := parseUint(s["tx-bits-per-second"])
	text := fmt.Sprintf("%s live: rx=%s, tx=%s",
		s["name"], humanBps(rx), humanBps(tx))
	if s["rx-drops-per-second"] != "" && s["rx-drops-per-second"] != "0" {
		text += fmt.Sprintf(", rx-drops/s=%s", s["rx-drops-per-second"])
	}
	if s["rx-errors-per-second"] != "" && s["rx-errors-per-second"] != "0" {
		text += fmt.Sprintf(", rx-errors/s=%s", s["rx-errors-per-second"])
	}
	return tool.Output{Data: []map[string]string{s}, Text: text}, nil
}

// getCustomerTraffic reads one PPPoE customer's live interface traffic. It never
// reads queues: RouterOS filters the active-session lookup by the exact identity,
// then monitor-traffic samples only the resolved interface once.
func (a *Adapter) getCustomerTraffic(ctx context.Context, args map[string]any) (tool.Output, error) {
	identity := firstString(args, "identity", "username", "name", "customer")
	if identity == "" {
		return tool.Output{Text: "UNKNOWN: identity pelanggan wajib untuk traffic PPPoE."}, nil
	}

	sessions, err := a.query(ctx, "/ppp/active/print", map[string]string{"?name": identity})
	if err != nil {
		return tool.Output{}, err
	}
	if len(sessions) != 1 || sessions[0]["name"] != identity || sessions[0]["interface"] == "" {
		return tool.Output{Text: fmt.Sprintf("UNKNOWN: sesi PPPoE untuk %q tidak dapat di-resolve secara unik.", identity)}, nil
	}

	iface := sessions[0]["interface"]
	rows, err := a.query(ctx, "/interface/monitor-traffic", map[string]string{
		"interface": iface,
		"once":      "",
	})
	if err != nil {
		return tool.Output{}, err
	}
	if len(rows) != 1 || rows[0]["name"] != iface {
		return tool.Output{Text: fmt.Sprintf("UNKNOWN: traffic interface PPPoE untuk %q tidak tersedia.", identity)}, nil
	}

	s := rows[0]
	text := fmt.Sprintf("Traffic PPPoE %q (%s): rx=%s, tx=%s", identity, iface,
		humanBps(parseUint(s["rx-bits-per-second"])), humanBps(parseUint(s["tx-bits-per-second"])))
	return tool.Output{Data: []map[string]string{s}, Text: text}, nil
}

// ---- Protokol biner RouterOS ----

// query: connect -> login -> kirim satu command -> baca sampai !done.
func (a *Adapter) query(ctx context.Context, cmd string, args map[string]string) ([]map[string]string, error) {
	conn, err := a.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	if err := a.login(r, w); err != nil {
		return nil, err
	}

	words := []string{cmd}
	for k, v := range args {
		words = append(words, "="+k+"="+v)
	}
	if err := writeSentence(w, words); err != nil {
		return nil, err
	}

	var rows []map[string]string
	for {
		sentence, err := readSentence(r)
		if err != nil {
			return nil, err
		}
		if len(sentence) == 0 {
			continue
		}
		switch sentence[0] {
		case "!done":
			return rows, nil
		case "!trap", "!fatal":
			return nil, fmt.Errorf("mikrotik: %s", joinSentence(sentence))
		case "!re":
			rows = append(rows, parseAttrs(sentence[1:]))
		}
	}
}

func (a *Adapter) connect(ctx context.Context) (net.Conn, error) {
	addr := a.addr()
	d := net.Dialer{Timeout: a.timeout}
	if a.cfg.TLS {
		// API-SSL: sertifikat self-signed MikroTik -> skip verify (curl -k).
		td := &tls.Dialer{
			NetDialer: &d,
			Config:    &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // disengaja
		}
		return td.DialContext(ctx, "tcp", addr)
	}
	return d.DialContext(ctx, "tcp", addr)
}

// login mengirim kredensial; menangani plaintext (6.43+) dan challenge MD5.
func (a *Adapter) login(r *bufio.Reader, w *bufio.Writer) error {
	if err := writeSentence(w, []string{"/login", "=name=" + a.cfg.User, "=password=" + a.cfg.Pass}); err != nil {
		return err
	}
	sentence, err := readSentence(r)
	if err != nil {
		return err
	}
	if len(sentence) == 0 {
		return fmt.Errorf("mikrotik: respons login kosong")
	}
	if sentence[0] == "!trap" {
		return fmt.Errorf("login gagal: %s", loginMessage(sentence))
	}
	if sentence[0] == "!done" {
		// Legacy (pra-6.43): ada =ret=<challenge> -> balas MD5.
		for _, word := range sentence[1:] {
			if strings.HasPrefix(word, "=ret=") {
				return a.legacyLogin(r, w, strings.TrimPrefix(word, "=ret="))
			}
		}
		return nil
	}
	return fmt.Errorf("login: respons tak dikenal: %s", joinSentence(sentence))
}

// legacyLogin menjawab challenge MD5 untuk RouterOS pra-6.43.
func (a *Adapter) legacyLogin(r *bufio.Reader, w *bufio.Writer, challengeHex string) error {
	challenge, err := hex.DecodeString(challengeHex)
	if err != nil {
		return fmt.Errorf("challenge login tidak valid: %v", err)
	}
	h := md5.New()
	h.Write([]byte{0x00})
	h.Write([]byte(a.cfg.Pass))
	h.Write(challenge)
	resp := "00" + hex.EncodeToString(h.Sum(nil))
	if err := writeSentence(w, []string{"/login", "=name=" + a.cfg.User, "=response=" + resp}); err != nil {
		return err
	}
	sentence, err := readSentence(r)
	if err != nil {
		return err
	}
	if len(sentence) > 0 && sentence[0] == "!done" {
		return nil
	}
	return fmt.Errorf("login (legacy) gagal: %s", joinSentence(sentence))
}

// ---- Encode/decode sentence ----

// writeSentence menulis satu sentence (deret word + terminator 0x00).
func writeSentence(w *bufio.Writer, words []string) error {
	for _, wd := range words {
		b := []byte(wd)
		if _, err := w.Write(encodeLength(len(b))); err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	if err := w.WriteByte(0x00); err != nil {
		return err
	}
	return w.Flush()
}

// readSentence membaca satu sentence sampai terminator (word panjang 0).
func readSentence(r *bufio.Reader) ([]string, error) {
	var words []string
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		var length int
		switch {
		case b&0x80 == 0x00:
			length = int(b)
		case b&0xC0 == 0x80:
			b2, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			length = int(b&0x3F)<<8 | int(b2)
		case b&0xE0 == 0xC0:
			b2, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			b3, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			length = int(b&0x1F)<<16 | int(b2)<<8 | int(b3)
		default:
			return nil, fmt.Errorf("panjang word tidak dikenal: 0x%02x", b)
		}
		if length == 0 {
			return words, nil
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, err
		}
		words = append(words, string(data))
	}
}

// encodeLength mengkode panjang word (variable-length, seperti UTF-8).
func encodeLength(n int) []byte {
	switch {
	case n < 0x80:
		return []byte{byte(n)}
	case n < 0x4000:
		return []byte{byte(n>>8) | 0x80, byte(n & 0xFF)}
	case n < 0x200000:
		return []byte{byte(n>>16) | 0xC0, byte(n >> 8 & 0xFF), byte(n & 0xFF)}
	default:
		panic("word mikrotik terlalu panjang")
	}
}

// parseAttrs mengubah word "=key=value" menjadi map.
func parseAttrs(words []string) map[string]string {
	m := map[string]string{}
	for _, w := range words {
		if !strings.HasPrefix(w, "=") {
			continue
		}
		kv := strings.SplitN(w[1:], "=", 2)
		if len(kv) == 2 {
			m[kv[0]] = kv[1]
		} else {
			m[kv[0]] = ""
		}
	}
	return m
}

// joinSentence menggabung word untuk pesan error.
func joinSentence(words []string) string {
	return strings.Join(words, " ")
}

// loginMessage mengekstrak teks =message= dari respons !trap.
func loginMessage(sentence []string) string {
	for _, w := range sentence {
		if strings.HasPrefix(w, "=message=") {
			return strings.TrimPrefix(w, "=message=")
		}
	}
	return joinSentence(sentence)
}

func firstRow(rows []map[string]string) map[string]string {
	if len(rows) == 0 {
		return map[string]string{}
	}
	return rows[0]
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// parseUint mengurai string angka (field RouterOS semuanya string) ke uint64.
func parseUint(s string) uint64 {
	var n uint64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}

// humanBytes mengubah byte ke bentuk terbaca (KB/MB/GB/TB).
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// humanBps mengubah bits-per-second ke bentuk terbaca (kbps/Mbps/Gbps).
func humanBps(n uint64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d bps", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cbps", float64(n)/float64(div), "kMGTPE"[exp])
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
