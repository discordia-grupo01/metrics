package health

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	StatusUnknown     = "unknown"
	StatusOperational = "operational"
	StatusDegraded    = "degraded"
)

type Target struct {
	Name string
	URL  string
}

type ServiceStatus struct {
	Name                 string    `json:"name"`
	Status               string    `json:"status"`
	LastCheckedAt        time.Time `json:"last_checked_at,omitempty"`
	ConsecutiveFailures  int       `json:"consecutive_failures,omitempty"`
	ConsecutiveSuccesses int       `json:"-"`
}

type Transition struct {
	Service             string
	PreviousStatus      string
	Status              string
	ConsecutiveFailures int
	OccurredAt          time.Time
}

type Monitor struct {
	targets           []Target
	client            *http.Client
	interval          time.Duration
	failureThreshold  int
	recoveryThreshold int

	mu           sync.RWMutex
	statuses     map[string]ServiceStatus
	onTransition func(context.Context, Transition)
}

func (m *Monitor) SetTransitionHandler(handler func(context.Context, Transition)) {
	m.onTransition = handler
}

func NewMonitor(targets []Target, client *http.Client, interval time.Duration, failureThreshold, recoveryThreshold int) *Monitor {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if failureThreshold <= 0 {
		failureThreshold = 2
	}
	if recoveryThreshold <= 0 {
		recoveryThreshold = 2
	}
	statuses := make(map[string]ServiceStatus, len(targets))
	for _, target := range targets {
		if target.Name == "" || target.URL == "" {
			continue
		}
		statuses[target.Name] = ServiceStatus{Name: target.Name, Status: StatusUnknown}
	}
	return &Monitor{
		targets:           targets,
		client:            client,
		interval:          interval,
		failureThreshold:  failureThreshold,
		recoveryThreshold: recoveryThreshold,
		statuses:          statuses,
	}
}

func (m *Monitor) Start(ctx context.Context) {
	m.poll(ctx)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.poll(ctx)
		}
	}
}

func (m *Monitor) Snapshot() []ServiceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ServiceStatus, 0, len(m.statuses))
	for _, target := range m.targets {
		if status, ok := m.statuses[target.Name]; ok {
			result = append(result, status)
		}
	}
	return result
}

func (m *Monitor) poll(ctx context.Context) {
	var wait sync.WaitGroup
	for _, target := range m.targets {
		target := target
		wait.Add(1)
		go func() {
			defer wait.Done()
			m.check(ctx, target)
		}()
	}
	wait.Wait()
}

func (m *Monitor) check(ctx context.Context, target Target) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
	if err == nil {
		response, requestErr := m.client.Do(request)
		if requestErr == nil {
			response.Body.Close()
			err = statusError(response.StatusCode)
		} else {
			err = requestErr
		}
	}

	m.mu.Lock()
	current, ok := m.statuses[target.Name]
	if !ok {
		m.mu.Unlock()
		return
	}
	current.LastCheckedAt = time.Now().UTC()
	previousStatus := current.Status
	if err != nil {
		current.ConsecutiveFailures++
		current.ConsecutiveSuccesses = 0
		if current.ConsecutiveFailures >= m.failureThreshold {
			current.Status = StatusDegraded
		}
	} else {
		current.ConsecutiveFailures = 0
		current.ConsecutiveSuccesses++
		if current.Status == StatusUnknown {
			current.Status = StatusOperational
		} else if current.Status == StatusDegraded && current.ConsecutiveSuccesses >= m.recoveryThreshold {
			current.Status = StatusOperational
		}
	}
	m.statuses[target.Name] = current
	m.mu.Unlock()

	if m.onTransition != nil &&
		current.Status != previousStatus &&
		(current.Status == StatusDegraded || previousStatus == StatusDegraded) {
		m.onTransition(ctx, Transition{
			Service:             target.Name,
			PreviousStatus:      previousStatus,
			Status:              current.Status,
			ConsecutiveFailures: current.ConsecutiveFailures,
			OccurredAt:          current.LastCheckedAt,
		})
	}
}

func statusError(statusCode int) error {
	if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
		return nil
	}
	return errors.New("health check returned non-success status")
}

func ParseTargets(value string) []Target {
	var targets []Target
	for _, raw := range strings.Split(value, ",") {
		nameURL := strings.SplitN(strings.TrimSpace(raw), "=", 2)
		if len(nameURL) != 2 {
			continue
		}
		name, url := strings.TrimSpace(nameURL[0]), strings.TrimSpace(nameURL[1])
		if name != "" && url != "" {
			targets = append(targets, Target{Name: name, URL: url})
		}
	}
	return targets
}
