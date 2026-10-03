package observability

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTeamCollectorAgregat(t *testing.T) {
	c := NewTeamCollector()
	c.Record(TeamDecision{Team: "noc", Handler: "daftar_pelanggan", HandledBy: "code", Tools: []string{"billing.list_customers"}, Latency: 100 * time.Millisecond, OK: true})
	c.Record(TeamDecision{Team: "noc", Handler: "daftar_pelanggan", HandledBy: "code", Latency: 300 * time.Millisecond, OK: false})
	c.Record(TeamDecision{Team: "cs", Handler: "llm", HandledBy: "llm", Latency: 2 * time.Second, OK: true})
	s := c.Snapshot()
	if s.Total != 3 || s.HandledBy["code"] != 2 || s.HandledBy["llm"] != 1 {
		t.Fatalf("agregat salah: %+v", s)
	}
	if s.ByTeam["noc"] != 2 || s.ByTeam["cs"] != 1 {
		t.Fatalf("per tim salah: %+v", s.ByTeam)
	}
	if s.ToolCalls["billing.list_customers"] != 1 {
		t.Fatalf("tool calls salah: %+v", s.ToolCalls)
	}
	if got := s.CodeShare; got < 0.66 || got > 0.67 {
		t.Fatalf("code_share = %v, mau ~0.667", got)
	}
	for _, r := range s.Rows {
		if r.Handler == "daftar_pelanggan" && (r.Count != 2 || r.Errors != 1 || r.AverageLatencyMS != 200) {
			t.Fatalf("baris daftar salah: %+v", r)
		}
	}
}

func TestTeamCollectorNilAman(t *testing.T) {
	var c *TeamCollector
	c.Record(TeamDecision{})
	c.RecordMismatch()
	if s := c.Snapshot(); s.Total != 0 || s.Rows == nil {
		t.Fatalf("nil collector harus memberi snapshot kosong non-nil: %+v", s)
	}
}

func TestTeamCollectorBalapGoroutine(t *testing.T) {
	c := NewTeamCollector()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				c.Record(TeamDecision{Team: "noc", Handler: "x", HandledBy: "code", OK: true})
				c.RecordMismatch()
				_ = c.Snapshot()
			}
		}()
	}
	wg.Wait()
	if s := c.Snapshot(); s.Total != 1000 || s.Mismatches != 1000 {
		t.Fatalf("hitungan balap salah: total=%d mismatch=%d", s.Total, s.Mismatches)
	}
}

// Metrik tidak boleh membawa data pribadi: struct hanya label + angka, dan
// hasil JSON tidak memuat field bernama nomor/pesan/username.
func TestTeamSnapshotTanpaDataPribadi(t *testing.T) {
	c := NewTeamCollector()
	c.Record(TeamDecision{Team: "noc", Handler: "cek_billing", HandledBy: "code", Tools: []string{"billing.get_customer"}, OK: true})
	b, _ := json.Marshal(c.Snapshot())
	low := strings.ToLower(string(b))
	for _, bad := range []string{"phone", "nomor", "message", "pesan", "username", "identity", "caller"} {
		if strings.Contains(low, bad) {
			t.Errorf("snapshot memuat kata %q: %s", bad, b)
		}
	}
}
