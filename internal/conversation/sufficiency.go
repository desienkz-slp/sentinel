// Package conversation mengelola kecukupan informasi pelanggan secara deterministik.
// Paket ini hanya menghasilkan pertanyaan yang aman untuk pelanggan; fakta live
// jaringan/perangkat tetap diperoleh oleh adaptor API.
package conversation

import "strings"

type Context struct {
	IdentityKnown bool
	Facts         map[string]string
}

type Plan struct {
	Intent  string
	Missing []string
	Ready   bool
}

var requirements = map[string][]string{
	"slow_connection": {"scope", "started", "condition"},
	"internet_down":   {"scope", "started", "condition"},
	"intermittent":    {"scope", "started", "condition"},
	"wifi_issue":      {"scope", "condition"},
}

// Evaluate determines readiness without asking for identity again or requesting
// operational data that belongs to Billing/RADIUS/MikroTik/GenieACS.
func Evaluate(intent string, ctx Context) Plan {
	intent = strings.TrimSpace(strings.ToLower(intent))
	if intent == "" {
		return Plan{Missing: []string{"problem"}}
	}
	req, ok := requirements[intent]
	if !ok {
		return Plan{Intent: intent, Missing: []string{"problem"}}
	}
	missing := make([]string, 0, len(req))
	for _, key := range req {
		if strings.TrimSpace(ctx.Facts[key]) == "" {
			missing = append(missing, key)
		}
	}
	return Plan{Intent: intent, Missing: missing, Ready: len(missing) == 0}
}
