package billing

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"ainoc/internal/tool"
)

// ToolList exposes aggregate counts only. It never returns customer records.
const ToolList = "billing.list_customers"

func (a *Adapter) total(ctx context.Context, query string) (int, error) {
	var out customersResponse
	if err := a.http.GetJSON(ctx, "/customers?per_page=1&page=1"+query, &out); err != nil {
		return 0, err
	}
	if out.Status != "success" {
		return 0, fmt.Errorf("respons billing tidak success: %s", out.Status)
	}
	return out.Meta.Total, nil
}

// listCustomers requests one explicit server page solely to read pagination
// metadata. Customer rows are deliberately discarded before they reach Output.
func (a *Adapter) listCustomers(ctx context.Context, args map[string]any) (tool.Output, error) {
	filter := strings.ToLower(firstString(args, "filter"))
	search := firstString(args, "search", "identity")

	var q, label string
	switch filter {
	case "", "ringkasan", "semua":
		if search == "" {
			return a.summary(ctx)
		}
		q, label = "&search="+url.QueryEscape(search), "Hasil pencarian"
	case "aktif", "active":
		q, label = "&status=active", "Pelanggan aktif"
	case "nonaktif", "inactive":
		q, label = "&status=inactive", "Pelanggan nonaktif"
	case "isolir", "isolated":
		q, label = "&is_isolated=1", "Pelanggan terisolir"
	case "cari", "search":
		if search == "" {
			return tool.Output{}, fmt.Errorf("filter cari butuh kata kunci (nama/username/nomor)")
		}
		q, label = "&search="+url.QueryEscape(search), "Hasil pencarian"
	default:
		return tool.Output{}, fmt.Errorf("filter %q tidak dikenal (aktif|nonaktif|isolir|cari)", filter)
	}

	n, err := a.total(ctx, q)
	if err != nil {
		return tool.Output{}, err
	}
	data := map[string]int{"count": n}
	return tool.Output{Data: data, Text: fmt.Sprintf("%s: %d pelanggan.", label, n)}, nil
}

func (a *Adapter) summary(ctx context.Context) (tool.Output, error) {
	type item struct{ label, q string }
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
	data := map[string]int{"total": vals[0], "aktif": vals[1], "nonaktif": vals[2], "isolir": vals[3]}
	return tool.Output{Data: data, Text: strings.TrimSpace(b.String())}, nil
}
