package service

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/discordia-grupo01/metrics/internal/model"
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

// The maximum range is 366 days: exactly that is allowed, a second more isn't.
func TestParsePeriodAllowsExactlyTheMaximumRange(t *testing.T) {
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	maxTo := from.Add(366 * 24 * time.Hour)
	if _, _, err := ParsePeriod(url.Values{"from": {from.Format(time.RFC3339)}, "to": {maxTo.Format(time.RFC3339)}}, time.Now()); err != nil {
		t.Errorf("366 days: %v", err)
	}
	overTo := maxTo.Add(time.Second)
	if _, _, err := ParsePeriod(url.Values{"from": {from.Format(time.RFC3339)}, "to": {overTo.Format(time.RFC3339)}}, time.Now()); err == nil {
		t.Error("366 days and a second should be rejected")
	}
}

func TestValidateIDRejectsMalformedUUID(t *testing.T) {
	if err := validateID("test"); !strings.Contains(err.Error(), "invalid id") {
		t.Fatalf("error = %v, want invalid id", err)
	}
	if err := validateID("00000000-0000-0000-0000-000000000000"); err != nil {
		t.Fatalf("valid UUID rejected: %v", err)
	}
}

func TestWriteCSVUsesServerSummaryColumns(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	response := httptest.NewRecorder()

	err := WriteCSV(response, model.ServerSummary{
		From:          from,
		To:            to,
		ActiveMembers: 3,
		MessagesSent:  12,
		NewMembers:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 {
		t.Fatalf("status = %d", response.Code)
	}
	if got, want := response.Header().Get("Content-Type"), "text/csv; charset=utf-8"; got != want {
		t.Fatalf("content type = %q, want %q", got, want)
	}
	if got, want := response.Body.String(),
		"from,to,active_members,messages_sent,new_members\n2026-10-01T00:00:00Z,2026-10-02T00:00:00Z,3,12,2\n"; got != want {
		t.Fatalf("CSV = %q, want %q", got, want)
	}
	if strings.Contains(response.Body.String(), "active_users") {
		t.Fatal("server CSV must not use global summary fields")
	}
}

func TestGranularityForUsesHoursUpToOneDay(t *testing.T) {
	from := time.Date(2026, 11, 3, 3, 0, 0, 0, time.UTC)
	cases := []struct {
		to       time.Time
		want     string
		wantStep time.Duration
	}{
		{from.Add(14 * time.Hour), model.GranularityHour, time.Hour},
		{from.Add(24 * time.Hour), model.GranularityHour, time.Hour},
		{from.Add(24*time.Hour + time.Minute), model.GranularityDay, 24 * time.Hour},
		{from.Add(90 * 24 * time.Hour), model.GranularityDay, 24 * time.Hour},
	}
	for _, tc := range cases {
		got, step := GranularityFor(from, tc.to)
		if got != tc.want || step != tc.wantStep {
			t.Fatalf("GranularityFor(%v) = %s/%v, want %s/%v", tc.to.Sub(from), got, step, tc.want, tc.wantStep)
		}
	}
}
