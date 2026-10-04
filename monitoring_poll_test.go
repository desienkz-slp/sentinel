package main

import (
	"context"
	"testing"
	"time"

	"ainoc/internal/config"
	"ainoc/internal/incident"
	"ainoc/internal/uplinkpoll"
)

type monitoringPollReader struct{ running bool }

func (r monitoringPollReader) InterfaceRunning(_ context.Context, names []string) (map[string]bool, error) {
	return map[string]bool{"wan1": r.running}, nil
}

func TestPollDerivedThreeTransitionsProjectReadOnlyAlert(t *testing.T) {
	store, err := incident.OpenMonitoringStore("", 50)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := uplinkpoll.New(store, map[string]uplinkpoll.InterfaceReader{
		"core-a": monitoringPollReader{running: true},
	}, []config.UplinkMonitor{{UplinkID: "core-a:wan1", Router: "core-a", Interface: "wan1"}})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	for i, running := range []bool{true, false, true, false} {
		producerReader := monitoringPollReader{running: running}
		producer, err = uplinkpoll.New(store, map[string]uplinkpoll.InterfaceReader{"core-a": producerReader}, []config.UplinkMonitor{{UplinkID: "core-a:wan1", Router: "core-a", Interface: "wan1"}})
		if err != nil || producer.Poll(context.Background(), at.Add(time.Duration(i)*time.Minute)) != nil {
			t.Fatalf("poll %d failed: %v", i, err)
		}
	}
	alerts := (&Server{monitor: store}).monitoringAlerts()
	if len(alerts) != 1 || alerts[0].Source != "monitoring" || alerts[0].Subject != "core-a:wan1" {
		t.Fatalf("alerts = %#v, want one read-only monitoring flap alert", alerts)
	}
}
