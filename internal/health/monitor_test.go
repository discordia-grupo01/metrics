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

func TestParseTargets(t *testing.T) {
	targets := ParseTargets("servers=http://servers:8080/readyz, bad, messaging=http://messaging:4000/readyz")
	if len(targets) != 2 || targets[0].Name != "servers" || targets[1].Name != "messaging" {
		t.Fatalf("targets = %#v", targets)
	}
}
