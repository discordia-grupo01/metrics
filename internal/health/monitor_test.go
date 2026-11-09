package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMonitorRequiresConsecutiveFailures(t *testing.T) {
	fail := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	monitor := NewMonitor([]Target{{Name: "servers", URL: server.URL}}, server.Client(), time.Minute, 2, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor.check(ctx, monitor.targets[0])
	if monitor.Snapshot()[0].Status != StatusUnknown {
		t.Fatal("first failure should not mark service degraded")
	}
	monitor.check(ctx, monitor.targets[0])
	if monitor.Snapshot()[0].Status != StatusDegraded {
		t.Fatal("second failure should mark service degraded")
	}

	fail = false
	monitor.check(ctx, monitor.targets[0])
	if monitor.Snapshot()[0].Status != StatusDegraded {
		t.Fatal("one successful check should not recover the service")
	}
	monitor.check(ctx, monitor.targets[0])
	if monitor.Snapshot()[0].Status != StatusOperational {
		t.Fatal("two successful checks should recover the service")
	}
}

func TestMonitorNotifiesDegradedAndRecoveryTransitions(t *testing.T) {
	fail := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var transitions []Transition
	monitor := NewMonitor([]Target{{Name: "servers", URL: server.URL}}, server.Client(), time.Minute, 2, 2)
	monitor.SetTransitionHandler(func(_ context.Context, transition Transition) {
		transitions = append(transitions, transition)
	})
	ctx := context.Background()

	monitor.check(ctx, monitor.targets[0])
	monitor.check(ctx, monitor.targets[0])
	fail = false
	monitor.check(ctx, monitor.targets[0])
	monitor.check(ctx, monitor.targets[0])

	if len(transitions) != 2 {
		t.Fatalf("transitions = %#v, want degraded and operational", transitions)
	}
	if transitions[0].PreviousStatus != StatusUnknown || transitions[0].Status != StatusDegraded {
		t.Fatalf("degraded transition = %#v", transitions[0])
	}
	if transitions[1].PreviousStatus != StatusDegraded || transitions[1].Status != StatusOperational {
		t.Fatalf("recovery transition = %#v", transitions[1])
	}
}

func TestParseTargets(t *testing.T) {
	targets := ParseTargets("servers=http://servers:8080/readyz, bad, messaging=http://messaging:4000/readyz")
	if len(targets) != 2 || targets[0].Name != "servers" || targets[1].Name != "messaging" {
		t.Fatalf("targets = %#v", targets)
	}
}

// Zero or negative settings fall back to the defaults; positive ones are kept.
func TestNewMonitorDefaults(t *testing.T) {
	for _, value := range []int{0, -1} {
		m := NewMonitor(nil, nil, time.Duration(value), value, value)
		if m.client.Timeout != 3*time.Second {
			t.Errorf("default client timeout = %v, want 3s", m.client.Timeout)
		}
		if m.interval != 30*time.Second || m.failureThreshold != 2 || m.recoveryThreshold != 2 {
			t.Errorf("settings %d: interval=%v failure=%d recovery=%d, want the defaults", value, m.interval, m.failureThreshold, m.recoveryThreshold)
		}
	}
	m := NewMonitor(nil, nil, time.Nanosecond, 1, 1)
	if m.interval != time.Nanosecond || m.failureThreshold != 1 || m.recoveryThreshold != 1 {
		t.Errorf("interval=%v failure=%d recovery=%d, want the given values", m.interval, m.failureThreshold, m.recoveryThreshold)
	}
}

// A target without name or without URL is not monitored.
func TestNewMonitorSkipsIncompleteTargets(t *testing.T) {
	m := NewMonitor([]Target{{Name: "", URL: "http://x/readyz"}, {Name: "servers", URL: "http://servers/readyz"}, {Name: "billing", URL: ""}, {Name: "identify", URL: "http://identify/readyz"}}, nil, time.Minute, 2, 2)
	got := m.Snapshot()
	if len(got) != 2 || got[0].Name != "servers" || got[1].Name != "identify" {
		t.Errorf("snapshot = %+v, want servers and identify", got)
	}
}

func TestParseTargetsSkipsEntriesWithoutNameOrURL(t *testing.T) {
	targets := ParseTargets("servers=http://servers/readyz,=http://x/readyz,billing=")
	if len(targets) != 1 || targets[0].Name != "servers" {
		t.Errorf("targets = %+v, want only servers", targets)
	}
}

// Every 2xx is healthy, and nothing else.
func TestStatusErrorAcceptsOnly2xx(t *testing.T) {
	for _, code := range []int{http.StatusOK, 299} {
		if err := statusError(code); err != nil {
			t.Errorf("%d: %v, want healthy", code, err)
		}
	}
	for _, code := range []int{199, http.StatusMultipleChoices, http.StatusServiceUnavailable} {
		if statusError(code) == nil {
			t.Errorf("%d should count as a failure", code)
		}
	}
}
