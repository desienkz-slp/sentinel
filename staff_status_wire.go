package main

import (
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
