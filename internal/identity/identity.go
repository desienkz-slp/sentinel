// Package identity menormalisasi identitas pengirim menjadi bentuk kanonik.
//
// Sesuai blueprint §20 (Intent System) dan §52 (Source of Truth): identitas
// pelanggan harus dinormalisasi ke satu bentuk supaya nomor yang sama dalam
// format berbeda (628123456789, +628123456789, 08123456789, @s.whatsapp.net,
// @lid, @c.us) dikenali sebagai satu pengirim yang sama — bukan dua entitas.
package identity

import (
	"regexp"
	"strings"
)

var nonDigit = regexp.MustCompile(`[^0-9]`)

// Normalize mengubah nomor telepon Indonesia ke bentuk kanonik E.164:
// "628xxxxxxxxxx" tanpa tanda +, spasi, atau suffix WhatsApp.
//
// Aturan:
//   - "08123456789"  -> "628123456789"  (0 di depan -> 62)
//   - "+628123456789"-> "628123456789"
//   - "628123456789@s.whatsapp.net" -> "628123456789"
//   - "8:628..."     -> "628..."
//   - "62xxxxxxxxx"  -> "62xxxxxxxxx" (sudah benar)
//
// Bila tidak bisa dinormalisasi (bukan nomor), mengembalikan string asli
// yang dibersihkan — jangan pernah menebak identitas yang tidak jelas.
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Buang suffix JID WhatsApp sebelum mengambil digit.
	s = strings.TrimSuffix(s, "@s.whatsapp.net")
	s = strings.TrimSuffix(s, "@c.us")
	s = strings.TrimSuffix(s, "@g.us")
	s = strings.TrimSuffix(s, "@lid")
	// Buang awalan "id:" atau "wa:" yang kadang muncul.
	if i := strings.Index(s, ":"); i >= 0 {
		if !strings.Contains(s[:i], "+") && i < 5 {
			s = s[i+1:]
		}
	}
	digits := nonDigit.ReplaceAllString(s, "")
	if digits == "" {
		return ""
	}
	// 0 di depan -> ganti 62 (Indonesia).
	if strings.HasPrefix(digits, "0") {
		digits = "62" + digits[1:]
	}
	// 8 di depan tanpa 62 -> asumsikan Indonesia.
	if strings.HasPrefix(digits, "8") && !strings.HasPrefix(digits, "62") {
		digits = "62" + digits
	}
	return digits
}

// Key mengembalikan kunci identitas kanonik untuk dipakai di sesi/memory/cache.
// Alias dari Normalize agar makna lebih jelas di titik pemakaian.
func Key(s string) string { return Normalize(s) }

// IsGroup melaporkan apakah JID menunjuk grup WhatsApp.
func IsGroup(chatID, sender string) bool {
	c := strings.ToLower(strings.TrimSpace(chatID))
	s := strings.ToLower(strings.TrimSpace(sender))
	return strings.HasSuffix(c, "@g.us") || strings.HasSuffix(s, "@g.us")
}

// IsLID melaporkan apakah identitas berupa @lid (Linked ID), bukan nomor.
func IsLID(s string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(s)), "@lid")
}

// Equal membandingkan dua identitas setelah normalisasi.
func Equal(a, b string) bool {
	na, nb := Normalize(a), Normalize(b)
	return na != "" && na == nb
}
