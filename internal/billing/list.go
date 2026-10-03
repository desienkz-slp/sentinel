package billing

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"ainoc/internal/tool"
)

// ToolList = daftar/ringkasan pelanggan (GET /api/noc/v1/customers dengan
// filter status/is_isolated/search). Read-only.
const ToolList = "billing.list_customers"

// listMaxRows membatasi baris yang ditampilkan agar balasan WA ringkas;
// totalnya tetap dilaporkan penuh dari meta API.
const listMaxRows = 15

func (a *Adapter) total(ctx context.Context, query string) (int, error) {
	var out customersResponse
	if err := a.http.GetJSON(ctx, "/customers?per_page=1"+query, &out); err != nil {
		return 0, err
	}
	if out.Status != "success" {
		return 0, fmt.Errorf("respons billing tidak success: %s", out.Status)
	}
	return out.Meta.Total, nil
}

// listCustomers: tanpa filter -> ringkasan jumlah (total/aktif/nonaktif/isolir).
// filter = "aktif" | "nonaktif" | "isolir" | "cari" (butuh search) -> daftar
// bernomor, maksimal listMaxRows baris + total sebenarnya.
func (a *Adapter) listCustomers(ctx context.Context, args map[string]any) (tool.Output, error) {
	filter := strings.ToLower(firstString(args, "filter"))
	search := firstString(args, "search", "identity")

	var q string
	var label string
	switch filter {
	case "", "ringkasan", "semua":
		if search == "" {
			return a.summary(ctx)
		}
		q, label = "&search="+url.QueryEscape(search), "pencarian "+search
	case "aktif", "active":
		q, label = "&status=active", "pelanggan aktif"
	case "nonaktif", "inactive":
		q, label = "&status=inactive", "pelanggan nonaktif"
	case "isolir", "isolated":
		q, label = "&is_isolated=1", "pelanggan terisolir"
	case "cari", "search":
		if search == "" {
			return tool.Output{}, fmt.Errorf("filter cari butuh kata kunci (nama/username/nomor)")
		}
		q, label = "&search="+url.QueryEscape(search), "pencarian "+search
	default:
		return tool.Output{}, fmt.Errorf("filter %q tidak dikenal (aktif|nonaktif|isolir|cari)", filter)
	}

	var out customersResponse
	path := fmt.Sprintf("/customers?per_page=%d%s", listMaxRows, q)
	if err := a.http.GetJSON(ctx, path, &out); err != nil {
		return tool.Output{}, err
	}
	if out.Status != "success" {
		return tool.Output{}, fmt.Errorf("respons billing tidak success: %s", out.Status)
	}
	if len(out.Data) == 0 {
		return tool.Output{Text: fmt.Sprintf("Tidak ada data untuk %s.", label)}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d pelanggan", strings.ToUpper(label[:1])+label[1:], out.Meta.Total)
	if out.Meta.Total > len(out.Data) {
		fmt.Fprintf(&b, " (menampilkan %d)", len(out.Data))
	}
	b.WriteString("\n")
	for i, c := range out.Data {
		fmt.Fprintf(&b, "%d. %s (%s) — %s", i+1, c.Name, c.Username, c.Status)
		if c.IsIsolated {
			b.WriteString(", ISOLIR")
		}
		if c.Package.Name != "" {
			fmt.Fprintf(&b, ", %s", c.Package.Name)
		}
		b.WriteString("\n")
	}
	if out.Meta.Total > len(out.Data) {
		b.WriteString("Sebutkan username untuk detail, atau persempit pencarian.")
	}
	return tool.Output{Data: out.Data, Text: strings.TrimSpace(b.String())}, nil
}

func (a *Adapter) summary(ctx context.Context) (tool.Output, error) {
	type item struct {
		label string
		q     string
	}
	items := []item{
		{"Total pelanggan", ""},
		{"Aktif", "&status=active"},
		{"Nonaktif", "&status=inactive"},
		{"Terisolir", "&is_isolated=1"},
	}
	vals := make([]int, len(items))
	for i, it := range items {
		n, err := a.total(ctx, it.q)
		if err != nil {
			return tool.Output{}, err
		}
		vals[i] = n
	}
	var b strings.Builder
	b.WriteString("Ringkasan pelanggan:\n")
	for i, it := range items {
		fmt.Fprintf(&b, "- %s: %d\n", it.label, vals[i])
	}
	b.WriteString("Ketik: daftar pelanggan isolir / aktif / nonaktif, atau cari <nama>.")
	data := map[string]int{"total": vals[0], "aktif": vals[1], "nonaktif": vals[2], "isolir": vals[3]}
	return tool.Output{Data: data, Text: strings.TrimSpace(b.String())}, nil
}
