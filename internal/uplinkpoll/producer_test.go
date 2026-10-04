package uplinkpoll

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"ainoc/internal/config"
	"ainoc/internal/incident"
)

type fakeReader struct {
	states map[string]bool
	err    error
}

func (r fakeReader) InterfaceRunning(_ context.Context, names []string) (map[string]bool, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make(map[string]bool, len(names))
	for _, name := range names {
		if state, ok := r.states[name]; ok {
			out[name] = state
		}
	}
	return out, nil
}

func testMapping() []config.UplinkMonitor {
	return []config.UplinkMonitor{{UplinkID: "core-a:wan1", Router: "core-a", Interface: "wan1"}}
}

func TestPollTransitionEmitsDeterministicPollDerivedEvent(t *testing.T) {
	store, err := incident.OpenMonitoringStore("", 50)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	p, err := New(store, map[string]InterfaceReader{"core-a": fakeReader{states: map[string]bool{"wan1": true}}}, testMapping())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Poll(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	p.readers["core-a"] = fakeReader{states: map[string]bool{"wan1": false}}
	if err := p.Poll(context.Background(), at.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	state := store.UplinkFlapState()
	if len(state.Observations) != 2 {
		t.Fatalf("observations = %#v, want baseline and transition", state.Observations)
	}
	event := state.Observations[1]
	if event.EventID != LocalEventID("core-a:wan1", incident.UplinkDown, at.Add(30*time.Second)) || event.Provenance != PollDerivedProvenance || event.State != incident.UplinkDown {
		t.Fatalf("event = %#v, want deterministic poll-derived DOWN transition", event)
	}
}

func TestPollSameStateProducesNoEvent(t *testing.T) {
	store, _ := incident.OpenMonitoringStore("", 50)
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	p, err := New(store, map[string]InterfaceReader{"core-a": fakeReader{states: map[string]bool{"wan1": true}}}, testMapping())
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Poll(context.Background(), at)
	_ = p.Poll(context.Background(), at.Add(30*time.Second))
	if got := len(store.UplinkFlapState().Observations); got != 1 {
		t.Fatalf("observations = %d, want baseline only", got)
	}
}

func TestPollUnknownOrUnmappedInterfaceIsIgnored(t *testing.T) {
	store, _ := incident.OpenMonitoringStore("", 50)
	p, err := New(store, map[string]InterfaceReader{"core-a": fakeReader{states: map[string]bool{"lan1": false}}}, testMapping())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Poll(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := len(store.UplinkFlapState().Observations); got != 0 {
		t.Fatalf("observations = %#v, want unmapped interface ignored", store.UplinkFlapState().Observations)
	}
}

func TestPollThreeTransitionsPersistFlapFinding(t *testing.T) {
	store, _ := incident.OpenMonitoringStore("", 50)
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	p, _ := New(store, map[string]InterfaceReader{"core-a": fakeReader{states: map[string]bool{"wan1": true}}}, testMapping())
	for i, running := range []bool{true, false, true, false} {
		p.readers["core-a"] = fakeReader{states: map[string]bool{"wan1": running}}
		if err := p.Poll(context.Background(), at.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	findings := store.UplinkFlapState().PriorFindings
	if len(findings) != 1 || findings[0].Transitions != 3 {
		t.Fatalf("findings = %#v, want one 3-transition flap", findings)
	}
}

func TestRestartPreservesEventDedupeAndCooldown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitoring.json")
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	store, _ := incident.OpenMonitoringStore(path, 50)
	p, _ := New(store, map[string]InterfaceReader{"core-a": fakeReader{states: map[string]bool{"wan1": true}}}, testMapping())
	for i, running := range []bool{true, false, true, false} {
		p.readers["core-a"] = fakeReader{states: map[string]bool{"wan1": running}}
		if err := p.Poll(context.Background(), at.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	store, err := incident.OpenMonitoringStore(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = New(store, map[string]InterfaceReader{"core-a": fakeReader{states: map[string]bool{"wan1": false}}}, testMapping())
	if err := p.Poll(context.Background(), at.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := len(store.UplinkFlapState().PriorFindings); got != 1 {
		t.Fatalf("findings after restart = %d, want cooldown preserved", got)
	}
	if got := len(store.UplinkFlapState().Observations); got != 4 {
		t.Fatalf("observations after restart = %d, want no duplicate event", got)
	}
}

func TestPollErrorRecordsUnknownWithoutFlap(t *testing.T) {
	store, _ := incident.OpenMonitoringStore("", 50)
	p, err := New(store, map[string]InterfaceReader{"core-a": fakeReader{err: errors.New("router unavailable")}}, testMapping())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Poll(context.Background(), time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	state := store.UplinkFlapState()
	if len(state.Observations) != 1 || state.Observations[0].State != incident.UplinkUnknown || state.Observations[0].EventID != "" || len(state.PriorFindings) != 0 {
		t.Fatalf("state = %#v, want UNKNOWN without event or flap", state)
	}
}

func TestSchedulerSeamUsesConfiguredDefaultWithoutStarting(t *testing.T) {
	if got := config.Default().UplinkPollInterval(); got != 30*time.Second {
		t.Fatalf("default interval = %s, want 30s", got)
	}
	if NewScheduler(nil, 0).Interval() != 30*time.Second {
		t.Fatal("scheduler did not normalize default interval")
	}
}
