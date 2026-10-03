package main

import (
	"context"
	"fmt"
	"strings"

	"ainoc/internal/directory"
)

// ============================================================================
//  Perintah staf lewat WhatsApp (NOC Senior / Admin / Super Admin).
//
//  Pesan dari nomor yang ada di Direktori Staf BUKAN keluhan pelanggan. Bila
//  isinya perintah cek ("cek user pppoe <user>", "cek traffic"), KODE yang
//  menjalankannya lewat gerbang yang sama dengan semua aksi (otorisasi role ->
//  audit -> dispatcher registry->policy->adapter) dan membalas data nyata.
//  Tidak ada LLM di jalur ini, jadi perilakunya sama untuk model apa pun.
// ============================================================================

const staffCmdOutputLimit = 900 // batas karakter per bagian agar pesan WA ringkas

// handleStaffCommand mengembalikan (balasan, true) bila pesan adalah perintah
// staf yang ditangani di sini. (balasan, false) berarti: lanjut alur biasa.
func (s *Server) handleStaffCommand(ctx context.Context, caller directory.Caller, sessKey, msg string) (string, bool) {
	if !caller.IsStaff || s.disp == nil {
		return "", false
	}
	cmd := directory.ParseCommand(msg)
	if cmd.Kind == directory.CmdNone {
		return "", false
	}

	target := cmd.Target
	if target == "" && s.sesi != nil {
		target = s.sesi.LastTarget(sessKey) // "cek traffic" setelah "cek user X" -> X
	}
	if target == "" {
		return "Username PPPoE-nya apa? Contoh: cek user pppoe namauser", true
	}

	var tools []string
	var title string
	switch cmd.Kind {
	case directory.CmdTraffic:
		title = "Traffic"
		tools = []string{"mikrotik.get_customer_traffic"}
	default:
		title = "PPPoE"
		tools = []string{"billing.get_customer", "radius.get_session", "mikrotik.get_pppoe_status"}
	}

	pinOK := s.pinSesi != nil && s.pinSesi.verified(caller.Number)
	var b strings.Builder
	fmt.Fprintf(&b, "*%s — %s*\n", title, target)
	anyData := false
	for _, tn := range tools {
		authz := directory.Authorize(caller, tn, pinOK)
		s.auditAction(caller, tn, map[string]any{"identity": target}, authz, pinOK)
		label := toolLabel(tn)
		if authz.Decision != directory.Allow {
			fmt.Fprintf(&b, "\n*%s:* tidak diizinkan untuk peran %s.\n", label, directory.Role(caller.Role).Label())
			continue
		}
		res := s.disp.Invoke(ctx, tn, map[string]any{"identity": target})
		s.auditActionResult(caller, tn, res.OK, string(res.Decision), res.Error)
		if !res.OK {
			fmt.Fprintf(&b, "\n*%s:* tidak tersedia (%s)\n", label, oneLineWA(res.Error))
			continue
		}
		anyData = true
		fmt.Fprintf(&b, "\n*%s:*\n%s\n", label, clip(strings.TrimSpace(res.Output.Text), staffCmdOutputLimit))
	}
	if !anyData {
		b.WriteString("\nBelum ada data yang bisa diambil — periksa koneksi/adaptor di dashboard.")
	}

	if s.sesi != nil {
		s.sesi.Append(sessKey, "user", msg, "STAFF_CMD", "", "")
		s.sesi.Append(sessKey, "assistant", fmt.Sprintf("(perintah staf %s untuk %s dijalankan)", title, target), "STAFF_CMD", "", target)
	}
	return strings.TrimSpace(b.String()), true
}

func toolLabel(tool string) string {
	switch tool {
	case "billing.get_customer":
		return "Billing"
	case "radius.get_session":
		return "RADIUS"
	case "mikrotik.get_pppoe_status":
		return "MikroTik"
	case "mikrotik.get_customer_traffic":
		return "Traffic"
	}
	return tool
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
