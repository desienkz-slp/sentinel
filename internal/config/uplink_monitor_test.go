package config

import (
	"testing"
	"time"
)

func TestValidateUplinkMonitorsRequiresUniqueConfiguredTargets(t *testing.T) {
	cfg := &Config{
		MikrotikRouters: []MikrotikRouter{{Name: "core-a", Host: "192.0.2.1", User: "noc"}},
		UplinkMonitors:  []UplinkMonitor{{UplinkID: "uplink-a", Router: "core-a", Interface: "wan1"}},
	}
	if err := cfg.ValidateUplinkMonitors(); err != nil {
		t.Fatalf("valid explicit mapping rejected: %v", err)
	}
	cfg.UplinkMonitors = append(cfg.UplinkMonitors, UplinkMonitor{UplinkID: "uplink-b", Router: "core-a", Interface: "wan1"})
	if err := cfg.ValidateUplinkMonitors(); err == nil {
		t.Fatal("duplicate router/interface mapping accepted")
	}
	cfg.UplinkMonitors = []UplinkMonitor{{UplinkID: "uplink-a", Router: "missing", Interface: "wan1"}}
	if err := cfg.ValidateUplinkMonitors(); err == nil {
		t.Fatal("unconfigured router mapping accepted")
	}
}

func TestUplinkPollIntervalDefaultsToThirtySeconds(t *testing.T) {
	if got := (&Config{}).UplinkPollInterval(); got != 30*time.Second {
		t.Fatalf("interval = %s, want 30s", got)
	}
	if got := (&Config{UplinkPollIntervalSec: 45}).UplinkPollInterval(); got != 45*time.Second {
		t.Fatalf("configured interval = %s, want 45s", got)
	}
}
