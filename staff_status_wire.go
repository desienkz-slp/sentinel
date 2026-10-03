package main

import (
	"ainoc/internal/directory"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// domainLabel = nama tampilan integrasi untuk balasan WhatsApp.
var domainLabel = map[string]string{
	"billing":  "Billing",
	"radius":   "RADIUS",
	"mikrotik": "MikroTik",
	"genieacs": "GenieACS",
}

// integrationStatusReply menjawab "sudah terhubung ke billing?" dari
// pengecekan kesehatan adaptor yang sebenarnya (read-only). Domain yang tidak
// terdaftar dilaporkan jujur sebagai belum dikonfigurasi.
func (s *Server) integrationStatusReply(ctx context.Context, domains []string, all bool) string {
	if all || len(domains) == 0 {
		domains = []string{"billing", "radius", "mikrotik", "genieacs"}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	lines := make([]string, len(domains))
	var wg sync.WaitGroup
	for i, d := range domains {
		wg.Add(1)
		go func(i int, d string) {
			defer wg.Done()
			label := domainLabel[d]
			if label == "" {
				label = d
			}
			h, found := s.disp.Health(ctx, d)
			switch {
			case !found:
				lines[i] = fmt.Sprintf("- %s: belum dikonfigurasi", label)
			case h.OK:
				lines[i] = fmt.Sprintf("- %s: terhubung (%d ms) — %s", label, h.LatencyMS, clip(oneLineWA(h.Detail), 160))
			default:
				lines[i] = fmt.Sprintf("- %s: GAGAL — %s", label, clip(oneLineWA(h.Detail), 160))
			}
		}(i, d)
	}
	wg.Wait()
	return "*Status integrasi*\n" + strings.Join(lines, "\n")
}

// customerListReply menjalankan billing.list_customers lewat gerbang
// otorisasi + audit yang sama dengan perintah staf lain.
func (s *Server) customerListReply(ctx context.Context, caller directory.Caller, sessKey, msg string, lq directory.ListQuery) string {
	const tn = "billing.list_customers"
	args := map[string]any{"filter": lq.Filter, "search": lq.Search}
	pinOK := s.pinSesi != nil && s.pinSesi.verified(caller.Number)
	authz := directory.Authorize(caller, tn, pinOK)
	s.auditAction(caller, tn, args, authz, pinOK)
	if authz.Decision != directory.Allow {
		return fmt.Sprintf("Daftar pelanggan tidak diizinkan untuk peran %s.", directory.Role(caller.Role).Label())
	}
	res := s.disp.Invoke(ctx, tn, args)
	s.auditActionResult(caller, tn, res.OK, string(res.Decision), res.Error)
	if !res.OK {
		return "Daftar pelanggan tidak tersedia (" + clip(oneLineWA(res.Error), 160) + ")"
	}
	out := strings.TrimSpace(res.Output.Text)
	if directory.HasHistoryWords(msg) {
		out += "\n\nRiwayat pembayaran per pelanggan: ketik *riwayat <username>* (tunggakan + bulan lunas)."
	}
	if s.sesi != nil {
		s.sesi.Append(sessKey, "user", msg, "STAFF_CMD", "", "")
		s.sesi.Append(sessKey, "assistant", "(perintah staf daftar pelanggan dijalankan)", "STAFF_CMD", "", "")
	}
	return out
}
