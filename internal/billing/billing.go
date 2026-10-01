// Package billing mengimplementasikan adapter read-only ke sistem billing NETORA.
//
// Kontrak (lihat NOC_AGENT_API.md §20-21):
//   - Base path : /api/noc/v1 (BUKAN /api/v1)
//   - Auth      : header X-NOC-API-Key: <64-hex> (BUKAN Bearer Sanctum)
//   - Data      : difilter otomatis per-tenant berdasarkan key.
//
// Adapter ini HANYA read-only. Tidak ada endpoint mutasi (isolir/payment)
// yang diekspos — tindakan WRITE lewat adaptor lain + gerbang policy.
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ainoc/internal/adapter"
	"ainoc/internal/tool"
)

// Customer adalah satu pelanggan dari /api/noc/v1/customers.
type Customer struct {
	ID            int     `json:"id"`
	UUID          string  `json:"uuid"`
	DisplayID     string  `json:"customer_id_display"`
	Name          string  `json:"name"`
	Phone         string  `json:"phone"`
	Email         string  `json:"email"`
	Address       string  `json:"address"`
	Username      string  `json:"username"`
	Status        string  `json:"status"`
	IsIsolated    bool    `json:"is_isolated"`
	IsolatedSince *string `json:"isolated_since"`
	JenisBayar    string  `json:"jenis_bayar"`
	AutoIsolir    bool    `json:"auto_isolir"`
	Area          struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"area"`
	Package struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		Price int    `json:"price"`
	} `json:"package"`
	Router struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Host string `json:"host"`
	} `json:"router"`
	Radius struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"radius"`
	Coordinate struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"coordinate"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type customersResponse struct {
	Status string     `json:"status"`
	Data   []Customer `json:"data"`
	Meta   struct {
		CurrentPage int `json:"current_page"`
		PerPage     int `json:"per_page"`
		Total       int `json:"total"`
		LastPage    int `json:"last_page"`
	} `json:"meta"`
}

// Adapter adalah adaptor billing read-only.
type Adapter struct {
	http *adapter.HTTP
}

// New membuat BillingAdapter. baseURL = NOC_BILLING_URL (mis. http://server),
// apiKey = NOC_BILLING_TOKEN (64-hex dari generate).
// Base path /api/noc/v1 ditambahkan otomatis.
func New(baseURL, apiKey string) *Adapter {
	if baseURL == "" {
		return &Adapter{http: adapter.New(adapter.Config{Domain: "billing"})}
	}
	full := strings.TrimRight(baseURL, "/") + "/api/noc/v1"
	return &Adapter{
		http: adapter.New(adapter.Config{
			Domain:     "billing",
			BaseURL:    full,
			Token:      apiKey,
			HeaderName: "X-NOC-API-Key",
			MaxRetries: 2,
		}),
	}
}

// Domain memenuhi tool.Adapter.
func (a *Adapter) Domain() string { return "billing" }

// Name memenuhi tool.Adapter.
func (a *Adapter) Name() string { return "NETORA Billing" }

// Configured memenuhi tool.Adapter.
func (a *Adapter) Configured() bool { return a.http != nil && a.http.Configured() }

// ToolNames memenuhi tool.Adapter.
func (a *Adapter) ToolNames() []string { return []string{"billing.get_customer"} }

// Health memenuhi tool.Adapter: probe dengan GET /customers?per_page=1.
func (a *Adapter) Health(ctx context.Context) (string, error) {
	d, err := a.Ping(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("billing menjawab (total pelanggan: %d)", d.TotalCustomers), nil
}

// PingResult adalah hasil verifikasi koneksi billing untuk dashboard.
type PingResult struct {
	BaseURL        string `json:"base_url"`
	LatencyMS      int64  `json:"ping_ms"`
	TotalCustomers int    `json:"total_customers"`
}

// Ping memverifikasi koneksi + auth terhadap NETORA dan mengembalikan detail
// yang bisa langsung ditampilkan operator (tanpa membocorkan token).
func (a *Adapter) Ping(ctx context.Context) (PingResult, error) {
	if !a.Configured() {
		return PingResult{}, fmt.Errorf("billing belum dikonfigurasi (set host + API key)")
	}
	start := time.Now()
	var out customersResponse
	if err := a.http.GetJSON(ctx, "/customers?per_page=1", &out); err != nil {
		return PingResult{}, err
	}
	if out.Status != "success" {
		return PingResult{}, fmt.Errorf("respons tidak success: %s", out.Status)
	}
	return PingResult{
		BaseURL:        a.http.BaseURL(),
		LatencyMS:      time.Since(start).Milliseconds(),
		TotalCustomers: out.Meta.Total,
	}, nil
}

// Invoke memenuhi tool.Adapter.
func (a *Adapter) Invoke(ctx context.Context, name string, args map[string]any) (tool.Output, error) {
	switch name {
	case "billing.get_customer":
		return a.getCustomer(ctx, args)
	default:
		return tool.Output{}, fmt.Errorf("tool billing tidak dikenal: %s", name)
	}
}

func (a *Adapter) getCustomer(ctx context.Context, args map[string]any) (tool.Output, error) {
	identity := firstString(args, "identity", "phone", "username", "search")
	if identity == "" {
		return tool.Output{}, fmt.Errorf("billing.get_customer butuh identity (nomor WA / username / nama)")
	}

	path := "/customers?search=" + urlQueryEscape(identity) + "&per_page=10"
	var out customersResponse
	if err := a.http.GetJSON(ctx, path, &out); err != nil {
		return tool.Output{}, err
	}
	if out.Status != "success" {
		return tool.Output{}, fmt.Errorf("respons billing tidak success: %s", out.Status)
	}

	if len(out.Data) == 0 {
		return tool.Output{
			Text: fmt.Sprintf("Tidak ditemukan pelanggan dengan identitas %q.", identity),
		}, nil
	}

	// Ringkas ke teks untuk LLM + data mentah untuk audit.
	var b strings.Builder
	fmt.Fprintf(&b, "Ditemukan %d pelanggan untuk %q:\n", len(out.Data), identity)
	for _, c := range out.Data {
		fmt.Fprintf(&b, "- %s (%s): status=%s", c.Name, c.Username, c.Status)
		if c.IsIsolated {
			b.WriteString(", ISOLIR")
		}
		if c.Phone != "" {
			fmt.Fprintf(&b, ", WA=%s", c.Phone)
		}
		if c.Package.Name != "" {
			fmt.Fprintf(&b, ", paket=%s", c.Package.Name)
		}
		if c.Area.Name != "" {
			fmt.Fprintf(&b, ", area=%s", c.Area.Name)
		}
		b.WriteString("\n")
	}
	if out.Meta.Total > len(out.Data) {
		fmt.Fprintf(&b, "(total %d, menampilkan %d teratas)\n", out.Meta.Total, len(out.Data))
	}

	return tool.Output{
		Data: out.Data,
		Text: strings.TrimSpace(b.String()),
	}, nil
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

func urlQueryEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, " ", "%20"), "&", "%26")
}

var _ = json.Valid // jaga import json tetap terpakai bila struktur berubah
