package directory

import (
	"regexp"
	"strings"
)

// Perintah serah-terima NOC -> CS (docs/PLAN_DUA_TIM_CS_NOC.md, Fase 4):
//
//	tutup CASE-20261003-ABC234 Gangguan sudah diperbaiki, silakan dicoba lagi
//	update CASE-20261003-ABC234 lapangan Teknisi menuju lokasi
//	update CASE-20261003-ABC234 Sedang kami periksa
//
// Pola sengaja sempit: wajib kata kerja di awal DAN ID kasus berformat tetap.

// HandoffCmd = hasil parsing perintah serah-terima.
type HandoffCmd struct {
	CaseID string // huruf besar
	Status string // selesai | diselidiki | butuh_lapangan | butuh_info
	Untuk  string // kalimat untuk pelanggan (boleh kosong, kecuali selesai)
}

var reCaseID = regexp.MustCompile(`(?i)^case-\d{8}-[a-z2-7]{6}$`)

// ParseHandoffCommand mengenali perintah serah-terima. ok=false bila bukan.
func ParseHandoffCommand(msg string) (HandoffCmd, bool) {
	fields := strings.Fields(strings.TrimSpace(msg))
	if len(fields) < 2 {
		return HandoffCmd{}, false
	}
	verb := strings.ToLower(strings.Trim(fields[0], ".,;:!?*_"))
	closing := false
	switch verb {
	case "tutup", "selesai", "close":
		closing = true
	case "update", "perbarui", "kabari":
	default:
		return HandoffCmd{}, false
	}
	id := strings.Trim(fields[1], ".,;:!?*_()[]")
	if !reCaseID.MatchString(id) {
		return HandoffCmd{}, false
	}
	rest := fields[2:]
	cmd := HandoffCmd{CaseID: strings.ToUpper(id)}
	if closing {
		cmd.Status = "selesai"
		cmd.Untuk = strings.TrimSpace(strings.Join(rest, " "))
		return cmd, true
	}
	cmd.Status = "diselidiki"
	if len(rest) > 0 {
		switch strings.ToLower(strings.Trim(rest[0], ".,;:!?*_")) {
		case "lapangan", "teknisi":
			cmd.Status, rest = "butuh_lapangan", rest[1:]
		case "info", "informasi":
			cmd.Status, rest = "butuh_info", rest[1:]
		case "selidiki", "diselidiki", "proses":
			rest = rest[1:]
		case "selesai", "tutup":
			cmd.Status, rest = "selesai", rest[1:]
		}
	}
	cmd.Untuk = strings.TrimSpace(strings.Join(rest, " "))
	return cmd, true
}
