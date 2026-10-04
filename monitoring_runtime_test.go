package main

import (
	"path/filepath"
	"testing"

	"ainoc/internal/config"
)

func TestMonitoringStorePathUsesIncidentDirectoryAndSafeAuditFallback(t *testing.T) {
	if got, want := monitoringStorePath(&config.Config{IncidentPath: filepath.Join("data", "incidents.json"), AuditPath: filepath.Join("other", "audit.json")}), filepath.Join("data", "monitoring.json"); got != want {
		t.Fatalf("incident-adjacent monitoring path = %q, want %q", got, want)
	}
	if got, want := monitoringStorePath(&config.Config{AuditPath: filepath.Join("data", "audit.json")}), filepath.Join("data", "monitoring.json"); got != want {
		t.Fatalf("audit fallback monitoring path = %q, want %q", got, want)
	}
}
