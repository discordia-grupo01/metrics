package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestMonitorIntegrationDegradesOnHTTPFailureAndTimeoutThenRecovers(t *testing.T) {
	var failing atomic.Bool
	failing.Store(true)
	var hanging atomic.Bool
	hanging.Store(true)

	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer failingServer.Close()

	hangingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hanging.Load() {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer hangingServer.Close()

	monitor := NewMonitor(
		[]Target{
			{Name: "failing", URL: failingServer.URL + "/readyz"},
			{Name: "hanging", URL: hangingServer.URL + "/readyz"},
		},
		&http.Client{Timeout: 20 * time.Millisecond},
		5*time.Millisecond,
		2,
		2,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go monitor.Start(ctx)

	waitForHealthStatus(t, monitor, "failing", StatusDegraded)
	waitForHealthStatus(t, monitor, "hanging", StatusDegraded)

	failing.Store(false)
	hanging.Store(false)

	waitForHealthStatus(t, monitor, "failing", StatusOperational)
	waitForHealthStatus(t, monitor, "hanging", StatusOperational)
}

func waitForHealthStatus(t *testing.T, monitor *Monitor, name, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, status := range monitor.Snapshot() {
			if status.Name == name && status.Status == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("service %q did not reach status %q; snapshot=%v", name, want, monitor.Snapshot())
}
