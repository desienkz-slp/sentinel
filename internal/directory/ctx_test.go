package directory

import (
	"context"
	"strings"
	"testing"
)

func TestPromptBlock(t *testing.T) {
	staf := Caller{Name: "Staf Uji", Role: RoleNOCSenior, IsStaff: true, Number: "628111"}
	b := staf.PromptBlock()
	for _, w := range []string{"STAF INTERNAL", "Staf Uji", "BUKAN pelanggan"} {
		if !strings.Contains(b, w) {
			t.Fatalf("blok staf tanpa %q: %s", w, b)
		}
	}
	if strings.Contains(b, "628111") {
		t.Fatal("nomor telepon tidak boleh masuk prompt")
	}
	cust := Caller{IsCustomer: true, Customer: &CustomerInfo{Name: "Budi"}}
	if !strings.Contains(cust.PromptBlock(), "Budi") {
		t.Fatal("blok pelanggan harus memuat nama")
	}
	if (Caller{}).PromptBlock() != "" {
		t.Fatal("penelepon tak dikenal harus tanpa blok")
	}
	ctx := WithCaller(context.Background(), staf)
	if c, ok := CallerFrom(ctx); !ok || c.Name != "Staf Uji" {
		t.Fatal("CallerFrom harus mengembalikan caller")
	}
}
