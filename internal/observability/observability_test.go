package observability

import (
	"testing"
	"time"
)

func TestCollectorMerekamHTTPDanMeringkasStatus(t *testing.T) {
	c := NewCollector()
	c.RecordHTTP("GET", "/api/health", 200, 10*time.Millisecond)
	c.RecordHTTP("GET", "/api/health", 503, 30*time.Millisecond)

	snapshot := c.Snapshot()
	if snapshot.TotalRequests != 2 {
		t.Fatalf("total request = %d, ingin 2", snapshot.TotalRequests)
	}
	if snapshot.ErrorRequests != 1 {
		t.Fatalf("total error = %d, ingin 1", snapshot.ErrorRequests)
	}
	if len(snapshot.Routes) != 1 || snapshot.Routes[0].Path != "/api/health" {
		t.Fatalf("ringkasan rute = %#v", snapshot.Routes)
	}
	if snapshot.Routes[0].AverageLatencyMS != 20 {
		t.Fatalf("rata-rata latensi = %d, ingin 20", snapshot.Routes[0].AverageLatencyMS)
	}
}

func TestKPIDariKasusTidakMengklaimResolusiTanpaVerifikasi(t *testing.T) {
	kpi := BuildKPI([]CaseOutcome{
		{ElapsedMS: 100, Verdict: "SEHAT"},
		{ElapsedMS: 300, Escalated: true},
		{ElapsedMS: 200, Error: "model gagal"},
	})

	if kpi.CasesProcessed != 3 || kpi.Escalated != 1 || kpi.Failed != 1 {
		t.Fatalf("KPI tidak sesuai: %#v", kpi)
	}
	if kpi.VerifiedResolved != 0 {
		t.Fatalf("resolusi tanpa verifikasi tidak boleh dihitung: %#v", kpi)
	}
	if kpi.AverageDiagnosisMS != 200 {
		t.Fatalf("rata-rata diagnosis = %d, ingin 200", kpi.AverageDiagnosisMS)
	}
}

func TestAlertsMengangkatDependensiTidakSehatDanKegagalanKasus(t *testing.T) {
	alerts := BuildAlerts([]Dependency{{Name: "llm", Status: "OFFLINE", Detail: "timeout"}}, []CaseOutcome{{Error: "model gagal"}})
	if len(alerts) != 2 {
		t.Fatalf("jumlah alert = %d, ingin 2: %#v", len(alerts), alerts)
	}
	if alerts[0].Source != "dependency" || alerts[1].Source != "case" {
		t.Fatalf("urutan/sumber alert salah: %#v", alerts)
	}
}
