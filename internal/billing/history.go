package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"ainoc/internal/tool"
)

// ToolHistory adalah tool riwayat tagihan bulanan
// (GET /api/noc/v1/customers/{id}/history, X-NOC-API-Key yang sama dengan
// billing.get_customer). Hanya GET — tidak ada jalur tulis dari adaptor ini.
const ToolHistory = "billing.get_history"

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
	Status string `json:"status"`
	Data   struct {
		// Dikelompokkan per tahun; wa_template sengaja tidak didekode.
		History     map[string][]historyItem `json:"history"`
		BillingInfo struct {
			JenisBayar string `json:"jenis_bayar"`
		} `json:"billing_info"`
	} `json:"data"`
}

// nowFn dipisah agar test bisa menetapkan "bulan berjalan".
var nowFn = time.Now

// bulanBerjalan = periode YYYY-MM menurut WIB (zona billing).
func bulanBerjalan() string {
	loc := time.FixedZone("WIB", 7*3600)
	return nowFn().In(loc).Format("2006-01")
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
	cust, err := a.resolveCustomer(ctx, identity)
	if err != nil {
		return tool.Output{}, err
	}

	var resp historyResponse
	if err := a.http.GetJSON(ctx, fmt.Sprintf("/customers/%d/history", cust.ID), &resp); err != nil {
		if strings.Contains(err.Error(), "http 404") {
			return tool.Output{}, fmt.Errorf("endpoint riwayat tagihan belum tersedia di server billing (Laravel Bill belum di-deploy versi yang memuat /customers/{id}/history)")
		}
		if strings.Contains(err.Error(), "http 401") || strings.Contains(err.Error(), "http 403") {
			return tool.Output{}, fmt.Errorf("API key billing ditolak (%s)", oneLine(err.Error()))
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

	// API memberi hingga 6 bulan ke DEPAN (tagihan di muka) — itu bukan
	// tunggakan. Tunggakan = unpaid dengan periode <= bulan berjalan.
	now := bulanBerjalan()
	var unpaid []historyItem
	var unpaidTotal float64
	paidAhead := 0
	lastPaid := ""
	for _, it := range all { // all sudah urut periode menurun
		if it.Status == "paid" && lastPaid == "" {
			lastPaid = it.Period
		}
		if it.Period > now {
			if it.Status == "paid" {
				paidAhead++
			}
			continue
		}
		if it.Status == "unpaid" {
			unpaid = append(unpaid, it)
			unpaidTotal += float64(it.ChargeAmount)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s) — riwayat tagihan (s/d %s):\n", cust.Name, cust.Username, now)
	if jb := resp.Data.BillingInfo.JenisBayar; jb != "" {
		fmt.Fprintf(&b, "Jenis bayar: %s\n", jb)
	}
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
	} else {
		b.WriteString("Belum ada periode lunas dalam riwayat.\n")
	}
	if paidAhead > 0 {
		fmt.Fprintf(&b, "Dibayar di muka: %d bulan\n", paidAhead)
	}
	b.WriteString("Terbaru:")
	shown := 0
	for _, it := range all {
		if it.Period > now {
			continue
		}
		if shown == 6 {
			break
		}
		st := "belum bayar"
		if it.Status == "paid" {
			st = "lunas"
		}
		fmt.Fprintf(&b, "\n- %s %s %s", it.Period, rupiah(float64(it.ChargeAmount)), st)
		shown++
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
