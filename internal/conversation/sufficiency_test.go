package conversation

import "testing"

func TestSlowConnectionNeedsScopeStartAndCondition(t *testing.T) {
	p := Evaluate("slow_connection", Context{IdentityKnown: true})
	if p.Ready || len(p.Missing) != 3 {
		t.Fatalf("plan=%+v, want three missing fields", p)
	}
	p = Evaluate("slow_connection", Context{IdentityKnown: true, Facts: map[string]string{
		"scope": "all_devices", "started": "today_afternoon", "condition": "available_but_slow",
	}})
	if !p.Ready || len(p.Missing) != 0 {
		t.Fatalf("complete plan=%+v", p)
	}
}

func TestDoesNotRequestKnownOrAPIDerivableFacts(t *testing.T) {
	p := Evaluate("slow_connection", Context{IdentityKnown: true, Facts: map[string]string{"scope": "all_devices"}})
	for _, field := range p.Missing {
		if field == "identity" || field == "onu_rx_power" {
			t.Fatalf("invalid customer question field %q", field)
		}
	}
}

func TestUnknownIntentWaitsForClarification(t *testing.T) {
	p := Evaluate("", Context{IdentityKnown: true})
	if p.Ready || len(p.Missing) != 1 || p.Missing[0] != "problem" {
		t.Fatalf("plan=%+v", p)
	}
}
