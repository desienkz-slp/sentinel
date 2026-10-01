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
	return PingResult{
		BaseURL:   a.addr(),
		LatencyMS: time.Since(start).Milliseconds(),
		Version:   r["version"],
		BoardName: r["board-name"],
		Uptime:    r["uptime"],
		CPU:       r["cpu-load"],
	}, nil
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

// getPPPoEStatus membaca sesi PPPoE aktif dari /ppp/active/print lalu
// mencocokkan username (name) dengan identitas yang dicari.
func (a *Adapter) getPPPoEStatus(ctx context.Context, args map[string]any) (tool.Output, error) {
	identity := firstString(args, "identity", "username", "name")
	if identity == "" {
		return tool.Output{}, fmt.Errorf("mikrotik.get_pppoe_status butuh identity (username PPPoE / nomor pelanggan)")
	}

	rows, err := a.query(ctx, "/ppp/active/print", nil)
	if err != nil {
		return tool.Output{}, err
	}

	var match []map[string]string
	for _, s := range rows {
		name := s["name"]
		if name == identity || strings.Contains(name, identity) {
			match = append(match, s)
		}
	}

	if len(match) == 0 {
		return tool.Output{
			Text: fmt.Sprintf("PPPoE %q TIDAK aktif (tidak ada sesi di /ppp/active).", identity),
		}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Sesi PPPoE aktif untuk %q: %d\n", identity, len(match))
	for _, s := range match {
		fmt.Fprintf(&b, "- %s: service=%s", s["name"], orDash(s["service"]))
		if s["caller-id"] != "" {
			fmt.Fprintf(&b, ", caller-id=%s", s["caller-id"])
		}
		if s["address"] != "" {
			fmt.Fprintf(&b, ", ip=%s", s["address"])
		}
		if s["uptime"] != "" {
			fmt.Fprintf(&b, ", uptime=%s", s["uptime"])
		}
		b.WriteString("\n")
	}

	return tool.Output{
		Data: match,
		Text: strings.TrimSpace(b.String()),
	}, nil
}

// getInterfaceStats membaca statistik kumulatif tiap interface (rx/tx byte,
// drop, error). Field terkonfirmasi: rx-byte, tx-byte, rx-drop, rx-error.
func (a *Adapter) getInterfaceStats(ctx context.Context, args map[string]any) (tool.Output, error) {
	rows, err := a.query(ctx, "/interface/print", nil)
	if err != nil {
		return tool.Output{}, err
	}
	filter := firstString(args, "interface", "name")
	var b strings.Builder
	count := 0
	for _, s := range rows {
		if s["type"] == "" {
			continue // bukan interface (biasanya VLAN/slave tanpa tipe)
		}
		if s["disabled"] == "true" || s["running"] == "false" {
			continue // interface mati/nonaktif
		}
		if filter != "" && s["name"] != filter {
			continue
		}
		// Lewati interface yang benar-benar nol (tanpa traffic).
		if s["rx-byte"] == "0" && s["tx-byte"] == "0" && filter == "" {
			continue
		}
		count++
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
		b.WriteString("\n")
	}
	if count == 0 {
		return tool.Output{Text: "Tidak ada interface (atau filter tidak cocok)."}, nil
	}
	return tool.Output{Data: rows, Text: strings.TrimSpace(b.String())}, nil
}

// getInterfaceLive membaca live bps via /interface/monitor-traffic once.
// Field terkonfirmasi: rx-bits-per-second, tx-bits-per-second, drops.
func (a *Adapter) getInterfaceLive(ctx context.Context, args map[string]any) (tool.Output, error) {
	iface := firstString(args, "interface", "name")
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
	return tool.Output{Data: rows, Text: text}, nil
}

// getCustomerTraffic membaca traffic per pelanggan dari /queue/simple (name
// berformat <pppoe-USER>). Field terkonfirmasi: bytes, rate, dropped, max-limit.
func (a *Adapter) getCustomerTraffic(ctx context.Context, args map[string]any) (tool.Output, error) {
	identity := firstString(args, "identity", "username", "name", "customer")
	rows, err := a.query(ctx, "/queue/simple/print", nil)
	if err != nil {
		return tool.Output{}, err
	}

	var match []map[string]string
	for _, q := range rows {
		name := trimQueueName(q["name"])
		if identity == "" || name == identity || strings.Contains(name, identity) {
			match = append(match, q)
		}
	}

	if len(match) == 0 {
		msg := "Tidak ada simple queue yang cocok."
		if identity != "" {
			msg = fmt.Sprintf("Tidak ada queue untuk pelanggan %q.", identity)
		}
		return tool.Output{Text: msg}, nil
	}

	var b strings.Builder
	if identity != "" {
		fmt.Fprintf(&b, "Traffic pelanggan %q: %d queue\n", identity, len(match))
	} else {
		fmt.Fprintf(&b, "Semua simple queue: %d\n", len(match))
	}
	for _, q := range match {
		user := trimQueueName(q["name"])
		rx, tx := splitPair(q["bytes"])
		rrx, rtx := splitPair(q["rate"])
		fmt.Fprintf(&b, "- %s: total rx=%s tx=%s", user, humanBytes(rx), humanBytes(tx))
		fmt.Fprintf(&b, ", live rx=%s tx=%s", humanBps(rrx), humanBps(rtx))
		if q["max-limit"] != "" {
			fmt.Fprintf(&b, ", max-limit=%s", q["max-limit"])
		}
		if q["dropped"] != "" && q["dropped"] != "0/0" {
			fmt.Fprintf(&b, ", dropped=%s", q["dropped"])
		}
		b.WriteString("\n")
	}
	return tool.Output{Data: match, Text: strings.TrimSpace(b.String())}, nil
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

// splitPair memecah "rx/tx" menjadi dua uint64 (default 0 bila kosong).
func splitPair(s string) (uint64, uint64) {
	a, b, _ := strings.Cut(s, "/")
	return parseUint(a), parseUint(b)
}

// trimQueueName membersihkan nama simple queue "<pppoe-USER>" -> "USER".
func trimQueueName(name string) string {
	name = strings.Trim(name, "<>")
	name = strings.TrimPrefix(name, "pppoe-")
	return name
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
