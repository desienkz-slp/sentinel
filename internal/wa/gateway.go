// Package wa menghubungkan ai-noc-go dengan WhatsApp Gateway (Node/Baileys).
//
// Kontrak yang dipakai (cocok dengan whatsapp/app/webhook.js dan app/server.js
// pada project ai-noc, TANPA perlu mengubah gateway tersebut):
//
//	MASUK  : gateway POST payload {message_id,chat_id,sender,sender_name,message,
//	         timestamp,type} ke NOC_WEBHOOK_URL. Bila respons memuat field
//	         "reply", gateway otomatis mengirimkannya kembali ke chat_id.
//	KELUAR : ai-noc-go POST {to,message} ke /api/whatsapp/send.
package wa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// InboundMessage adalah payload yang dikirim gateway ke kita.
type InboundMessage struct {
	MessageID  string `json:"message_id"`
	ChatID     string `json:"chat_id"`
	Sender     string `json:"sender"`
	SenderName string `json:"sender_name"`
	Message    string `json:"message"`
	Timestamp  string `json:"timestamp"`
	Type       string `json:"type"`
}

// Identity mengembalikan identitas pengirim untuk keperluan blocklist, sesi,
// cache, dan memory.
//
// PENTING: utamakan Sender (nomor telepon hasil resolusi gateway), bukan
// ChatID. WhatsApp kini mengirim sebagian chat sebagai @lid (Linked ID) —
// deretan angka panjang yang BUKAN nomor telepon. Memakai ChatID membuat nomor
// tidak cocok dengan blocklist sehingga pesan pelanggan yang diblokir ikut
// lolos.
func (m InboundMessage) Identity() string {
	if s := strings.TrimSpace(m.Sender); s != "" && !IsLID(s) {
		return s
	}
	// ChatID adalah nomor telepon asli (bukan LID) — pakai itu.
	if c := strings.TrimSpace(m.ChatID); c != "" && !IsLID(c) {
		return c
	}
	// Keduanya LID/tidak diketahui: kirim apa adanya supaya tetap tercatat
	// dan bisa didiagnosis dari log.
	if s := strings.TrimSpace(m.Sender); s != "" {
		return s
	}
	return strings.TrimSpace(m.ChatID)
}

// IsLID melaporkan apakah identitas berupa @lid (Linked ID) — deretan angka
// panjang yang bukan nomor telepon.
func IsLID(v string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(v)), "@lid")
}

// Reply adalah respons yang kita kembalikan ke gateway.
// Bila Reply terisi, gateway otomatis mengirimkannya ke WhatsApp.
type Reply struct {
	Accepted   bool    `json:"accepted"`
	MessageID  string  `json:"message_id,omitempty"`
	Reply      string  `json:"reply,omitempty"`
	Report     string  `json:"report,omitempty"`
	Verdict    string  `json:"verdict,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Engine     string  `json:"engine,omitempty"`
	ElapsedMS  int64   `json:"elapsed_ms,omitempty"`
	// Case ID + state dari Case Engine (fase 1) untuk observasi gateway/audit.
	CaseID    string `json:"case_id,omitempty"`
	CaseState string `json:"case_state,omitempty"`
	Note      string `json:"note,omitempty"`
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(baseURL string, timeoutSec int) *Client {
	if timeoutSec <= 0 {
		timeoutSec = 45
	}
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		HTTP:    &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
	}
}

func (c *Client) Enabled() bool { return c != nil && c.BaseURL != "" }

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	if !c.Enabled() {
		return fmt.Errorf("whatsapp gateway belum dikonfigurasi (set NOC_WA_BASE_URL)")
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("gateway tidak terjangkau: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("gateway HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("respons gateway bukan JSON valid: %w (%s)", err, truncate(string(raw), 200))
	}
	return nil
}

// Send mengirim pesan WhatsApp. Ini jalur keluar utama (notifikasi & balasan async).
func (c *Client) Send(ctx context.Context, to, message string) (map[string]any, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		return nil, fmt.Errorf("nomor tujuan kosong")
	}
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("pesan kosong")
	}
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/api/whatsapp/send", map[string]string{"to": to, "message": message}, &out)
	return out, err
}

// Status mengembalikan status sesi gateway (connected/connecting/disconnected + nomor).
func (c *Client) Status(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodGet, "/api/whatsapp/status", nil, &out)
	return out, err
}

// QR mengambil QR pairing untuk login WhatsApp.
func (c *Client) QR(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodGet, "/api/whatsapp/qr", nil, &out)
	return out, err
}

func (c *Client) Connect(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/api/whatsapp/connect", map[string]any{}, &out)
	return out, err
}

// Reconnect menyambung ulang sesi WhatsApp tanpa menghapus kredensial.
func (c *Client) Reconnect(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/api/whatsapp/reconnect", map[string]any{}, &out)
	return out, err
}

func (c *Client) Logout(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/api/whatsapp/logout", map[string]any{}, &out)
	return out, err
}

// Connected melaporkan apakah sesi WhatsApp siap menerima/mengirim pesan.
func (c *Client) Connected(ctx context.Context) bool {
	st, err := c.Status(ctx)
	if err != nil {
		return false
	}
	return isConnected(st)
}

// isConnected membaca status dari beberapa bentuk respons yang mungkin.
func isConnected(st map[string]any) bool {
	for _, k := range []string{"status", "state", "connection", "session_status"} {
		v, ok := st[k]
		if !ok {
			continue
		}
		s, _ := v.(string)
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "connected", "open", "ready", "online", "authenticated":
			return true
		}
	}
	for _, k := range []string{"connected", "is_connected", "ready"} {
		if b, ok := st[k].(bool); ok {
			return b
		}
	}
	return false
}

// IsGroup melaporkan apakah pesan berasal dari grup WhatsApp.
// JID grup selalu berakhiran "@g.us"; JID pengguna "@s.whatsapp.net".
func IsGroup(chatID, sender string) bool {
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(chatID)), "@g.us") {
		return true
	}
	// Beberapa versi Baileys mengirim JID grup pada field sender.
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(sender)), "@g.us")
}

// Blocked memeriksa apakah pengirim ada di blocklist. Nomor dalam blocklist
// TIDAK dibalas. Daftar kosong = tidak ada yang diblokir (semua dibalas).
func Blocked(blocklist []string, identity string) bool {
	id := normalize(identity)
	for _, b := range blocklist {
		if normalize(b) == id && id != "" {
			return true
		}
	}
	return false
}

// normalize menyamakan format nomor: buang karakter non-digit kecuali awalan +.
func normalize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "@s.whatsapp.net")
	s = strings.TrimSuffix(s, "@c.us")
	s = strings.TrimSuffix(s, "@g.us")
	var b strings.Builder
	for i, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if r == '+' && i == 0 {
			continue
		}
	}
	return b.String()
}

// ParseBlocklist memecah "628xx,628yy" menjadi slice.
func ParseBlocklist(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
