package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"ainoc/internal/adapter"
	"ainoc/internal/tool"
)

// ToolHistory adalah tool riwayat tagihan bulanan (GET /api/v1/customers/{id}/history).
// Endpoint ini hanya ada di API Sanctum (/api/v1), bukan di /api/noc/v1, sehingga
// butuh token Bearer terpisah. Hanya satu path GET yang dipanggil — tidak ada
// jalur tulis (isolir/payments) dari adaptor ini.
const ToolHistory = "billing.get_history"

// WithUserToken mengaktifkan tool riwayat tagihan memakai token Bearer (Sanctum).
// Token kosong = tool tetap terdaftar tetapi menjawab "belum dikonfigurasi".
func (a *Adapter) WithUserToken(baseURL, token string) *Adapter {
	token = strings.TrimSpace(token)
	if baseURL == "" || token == "" {
		a.v1 = nil
		return a
	}
	a.v1 = adapter.New(adapter.Config{
		Domain:     "billing",
		BaseURL:    strings.TrimRight(baseURL, "/") + "/api/v1",
		Token:      token, // HeaderName default = Authorization: Bearer
		MaxRetries: 2,
	})
	return a
}

// HistoryEnabled melaporkan apakah token Bearer terpasang.
func (a *Adapter) HistoryEnabled() bool { return a.v1 != nil }

// flexNum menerima angka JSON berupa number maupun string ("150000.00").
type flexNum float64

func (n *flexNum) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*n = flexNum(f)
	return nil
}

type historyItem struct {
	Period       string  `json:"period"`
	ChargeAmount flexNum `json:"charge_amount"`
	Status       string  `json:"status"`
}

type historyResponse struct {
	Success bool `json:"success"`
	Data    struct {
		// Dikelompokkan per tahun; wa_template sengaja tidak didekode.
		History map[string][]historyItem `json:"history"`
	} `json:"data"`
}

// resolveCustomer mencari pelanggan lewat NOC search. Username persis menang;
// bila hanya satu hasil, pakai itu; selain itu minta pengguna lebih spesifik.
func (a *Adapter) resolveCustomer(ctx context.Context, identity string) (Customer, error) {
	var out customersResponse
	path := "/customers?search=" + url.QueryEscape(identity) + "&per_page=10"
	if err := a.http.GetJSON(ctx, path, &out); err != nil {
		return Customer{}, err
	}
	for _, c := range out.Data {
		if strings.EqualFold(c.Username, identity) {
			return c, nil
		}
	}
	switch len(out.Data) {
	case 0:
		return Customer{}, fmt.Errorf("pelanggan %q tidak ditemukan", identity)
	case 1:
		return out.Data[0], nil
	}
	return Customer{}, fmt.Errorf("%d pelanggan cocok dengan %q — sebutkan username persisnya", len(out.Data), identity)
}

func rupiah(v float64) string {
	s := strconv.FormatInt(int64(v+0.5), 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return "Rp" + s
}

func (a *Adapter) getHistory(ctx context.Context, args map[string]any) (tool.Output, error) {
	identity := firstString(args, "identity", "username", "search")
	if identity == "" {
		return tool.Output{}, fmt.Errorf("billing.get_history butuh identity (username pelanggan)")
	}
	if a.v1 == nil {
		return tool.Output{}, fmt.Errorf("riwayat tagihan belum dikonfigurasi: isi Token Billing (Bearer) di Pengaturan")
	}
	cust, err := a.resolveCustomer(ctx, identity)
	if err != nil {
		return tool.Output{}, err
	}

	var resp historyResponse
	if err := a.v1.GetJSON(ctx, fmt.Sprintf("/customers/%d/history", cust.ID), &resp); err != nil {
		if strings.Contains(err.Error(), "http 401") || strings.Contains(err.Error(), "http 403") {
			return tool.Output{}, fmt.Errorf("token Bearer billing ditolak/kedaluwarsa (%s)", oneLine(err.Error()))
		}
		return tool.Output{}, err
	}

	var all []historyItem
	for _, items := range resp.Data.History {
		all = append(all, items...)
	}
	if len(all) == 0 {
		return tool.Output{Text: fmt.Sprintf("%s (%s): belum ada riwayat tagihan.", cust.Name, cust.Username)}, nil
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Period > all[j].Period })

	var unpaid []historyItem
	var unpaidTotal float64
	lastPaid := ""
	for _, it := range all {
		switch it.Status {
		case "unpaid":
			unpaid = append(unpaid, it)
			unpaidTotal += float64(it.ChargeAmount)
		case "paid":
			if lastPaid == "" {
				lastPaid = it.Period
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s) — riwayat tagihan:\n", cust.Name, cust.Username)
	if len(unpaid) == 0 {
		b.WriteString("Tidak ada tunggakan.\n")
	} else {
		fmt.Fprintf(&b, "TUNGGAKAN %d bulan, total %s:\n", len(unpaid), rupiah(unpaidTotal))
		for _, it := range unpaid {
			fmt.Fprintf(&b, "- %s: %s (belum bayar)\n", it.Period, rupiah(float64(it.ChargeAmount)))
		}
	}
	if lastPaid != "" {
		fmt.Fprintf(&b, "Terakhir lunas: %s\n", lastPaid)
	}
	b.WriteString("Terbaru:")
	n := len(all)
	if n > 6 {
		n = 6
	}
	for _, it := range all[:n] {
		st := it.Status
		if st == "paid" {
			st = "lunas"
		} else if st == "unpaid" {
			st = "belum bayar"
		}
		fmt.Fprintf(&b, "\n- %s %s %s", it.Period, rupiah(float64(it.ChargeAmount)), st)
	}

	raw, _ := json.Marshal(all)
	var data any
	_ = json.Unmarshal(raw, &data)
	return tool.Output{Data: data, Text: strings.TrimSpace(b.String())}, nil
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
