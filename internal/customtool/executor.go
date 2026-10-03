package customtool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ainoc/internal/adapter"
	"ainoc/internal/tool"
)

// Executor mengeksekusi spec yang sudah ACTIVE sebagai tool read-only, memakai
// base URL + auth domain yang sudah dikonfigurasi sistem. Ini jembatan antara
// spec deklaratif (disetujui superadmin) dan dispatcher tool yang ada.
//
// Keamanan: Executor hanya bisa melakukan GET ke path RELATIF pada domain yang
// diizinkan (billing/radius/mikrotik/genieacs). Tidak ada domain baru, tidak
// ada method selain GET, tidak ada path traversal (sudah divalidasi di Validate).
type Executor struct {
	// clients memetakan domain -> base adapter.HTTP dengan auth domain itu.
	clients map[string]*adapter.HTTP
}

// NewExecutor membangun executor dari konfigurasi domain yang sudah ada.
// domainConf: domain -> adapter.Config (base URL + auth) yang SAMA dipakai
// adapter resmi. Hanya domain yang benar-benar terkonfigurasi yang ikut.
func NewExecutor(domainConf map[string]adapter.Config) *Executor {
	clients := map[string]*adapter.HTTP{}
	for domain, cfg := range domainConf {
		if !AllowedDomains[domain] {
			continue
		}
		if strings.TrimSpace(cfg.BaseURL) == "" {
			continue // domain belum terkonfigurasi -> tool custom tak bisa jalan
		}
		cfg.Domain = domain
		clients[domain] = adapter.New(cfg)
	}
	return &Executor{clients: clients}
}

// Resolve mengubah spec active menjadi tool.Adapter generik. Hanya spec
// ber-status Active yang boleh dijalankan.
func (e *Executor) Resolve(spec Spec) (tool.Adapter, error) {
	if spec.Status != StatusActive {
		return nil, fmt.Errorf("tool %q belum disetujui (status=%s)", spec.Name, spec.Status)
	}
	if err := Validate(spec); err != nil {
		return nil, err
	}
	cl, ok := e.clients[spec.Domain]
	if !ok {
		return nil, fmt.Errorf("domain %q belum terkonfigurasi", spec.Domain)
	}
	return &specAdapter{spec: spec, http: cl}, nil
}

// specAdapter adalah adapter generik untuk satu spec read-only.
type specAdapter struct {
	spec Spec
	http *adapter.HTTP
}

func (a *specAdapter) Domain() string   { return a.spec.Domain }
func (a *specAdapter) Name() string     { return a.spec.Name }
func (a *specAdapter) Configured() bool { return a.http != nil && a.http.Configured() }
func (a *specAdapter) ToolNames() []string {
	return []string{a.spec.Name}
}
func (a *specAdapter) Health(ctx context.Context) (string, error) {
	return a.http.Health(ctx, a.spec.Path)
}

// Invoke mengeksekusi GET ke path spec (substitusi {param} dari args), lalu
// mengekstrak field yang diminta. Output = teks ringkas + data mentah.
func (a *specAdapter) Invoke(ctx context.Context, name string, args map[string]any) (tool.Output, error) {
	if name != a.spec.Name {
		return tool.Output{}, fmt.Errorf("tool tidak dikenal: %s", name)
	}
	path := a.spec.Path
	// Substitusi {identity} / {param} dari args ke path.
	path = substituteParams(path, args)

	var raw map[string]any
	if err := a.http.GetJSON(ctx, path, &raw); err != nil {
		return tool.Output{}, err
	}

	out := tool.Output{Data: raw}
	if a.spec.Extract != "" {
		val, ok := extractJSONPath(raw, a.spec.Extract)
		if !ok {
			out.Text = fmt.Sprintf("%s: field %q tidak ditemukan", a.spec.Name, a.spec.Extract)
		} else {
			out.Text = fmt.Sprintf("%s: %s", a.spec.Name, stringify(val))
		}
	} else {
		b, _ := json.Marshal(raw)
		out.Text = fmt.Sprintf("%s: %s", a.spec.Name, string(b))
	}
	return out, nil
}

// substituteParams mengganti {key} pada path dengan nilai args[key].
// Nilai tanpa placeholder dibiarkan. Placeholder tanpa arg -> dikosongkan.
func substituteParams(path string, args map[string]any) string {
	out := path
	for k, v := range args {
		token := "{" + k + "}"
		if !strings.Contains(out, token) {
			continue
		}
		sv := ""
		if v != nil {
			sv = stringify(v)
		}
		out = strings.ReplaceAll(out, token, sv)
	}
	return out
}

// extractJSONPath mengambil nilai dari JSON nested lewat jalur dot/bracket
// sederhana (mis. "data.0.name" atau "user.phone"). Aman: hanya navigasi.
func extractJSONPath(raw map[string]any, path string) (any, bool) {
	var cur any = raw
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		// bracket index seperti "0" atau "foo[0]" -> navigasi array.
		idx := -1
		key := part
		if i := strings.Index(part, "["); i >= 0 && strings.HasSuffix(part, "]") {
			key = part[:i]
			var n int
			if _, err := fmt.Sscanf(part[i+1:len(part)-1], "%d", &n); err == nil {
				idx = n
			}
		}
		switch node := cur.(type) {
		case map[string]any:
			if key != "" {
				cur, _ = node[key]
			}
		case []any:
			if idx >= 0 && idx < len(node) {
				cur = node[idx]
			}
		}
		if cur == nil {
			return nil, false
		}
	}
	return cur, true
}

// stringify mengubah nilai ke string ringkas (untuk teks output).
func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%v", t)
	case bool:
		return fmt.Sprintf("%v", t)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}
