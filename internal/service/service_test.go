package service

import (
	"net/url"
	"testing"
	"time"
)

func TestParsePeriodDefaultsToLastThirtyDays(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	from, to, err := ParsePeriod(url.Values{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !to.Equal(now) || !from.Equal(now.Add(-30*24*time.Hour)) {
		t.Fatalf("unexpected period: %v - %v", from, to)
	}
}

func TestParsePeriodRejectsInvalidOrTooLongRanges(t *testing.T) {
	_, _, err := ParsePeriod(url.Values{"from": []string{"2026-01-02T00:00:00Z"}, "to": []string{"2026-01-01T00:00:00Z"}}, time.Now())
	if err == nil {
		t.Fatal("expected reversed range error")
	}
	_, _, err = ParsePeriod(url.Values{"from": []string{"2024-01-01T00:00:00Z"}, "to": []string{"2026-01-01T00:00:00Z"}}, time.Now())
	if err == nil {
		t.Fatal("expected maximum range error")
	}
}
