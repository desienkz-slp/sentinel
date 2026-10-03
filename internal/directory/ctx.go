package directory

import (
	"context"
	"strings"
)

type callerCtxKey struct{}

// WithCaller menanam Caller ke context agar agen/handler hilir tahu siapa dan
// perannya.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerCtxKey{}, c)
}

// CallerFrom mengambil Caller dari context; ok=false bila tidak ada.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerCtxKey{}).(Caller)
	return c, ok
}

// PromptBlock merangkum identitas penelepon untuk disisipkan ke prompt LLM.
// Isinya fakta dari direktori/billing (diputuskan KODE), jadi model tidak perlu
// menebak atau bertanya "siapa Anda". Tidak memuat nomor telepon.
func (c Caller) PromptBlock() string {
	switch {
	case c.IsStaff:
		nama := strings.TrimSpace(c.Name)
		if nama == "" {
			nama = "staf internal"
		}
		return "\n\n[IDENTITAS PENGIRIM — terverifikasi sistem]\n" +
			"Pengirim adalah STAF INTERNAL ISP: " + nama + ", jabatan " + c.Role.Label() +
			". Dia BUKAN pelanggan. Panggil dengan namanya. Jangan tanya siapa dia, " +
			"jangan tanya \"kendala apa yang dialami\", dan jangan memakai nada layanan pelanggan. " +
			"Bila ditanya identitasnya, jawab nama dan jabatan di atas."
	case c.IsCustomer && c.Customer != nil:
		return "\n\n[IDENTITAS PENGIRIM — terverifikasi sistem]\n" +
			"Pengirim adalah pelanggan terdaftar: " + strings.TrimSpace(c.Customer.Name) +
			". Boleh dipanggil dengan nama tersebut. Jangan minta nomor/ID yang sudah diketahui."
	}
	return ""
}
