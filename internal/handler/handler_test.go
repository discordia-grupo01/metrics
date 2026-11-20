package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/discordia-grupo01/metrics/internal/model"
	"github.com/discordia-grupo01/metrics/internal/repository"
	"github.com/discordia-grupo01/metrics/internal/service"
	"github.com/gin-gonic/gin"
)

func TestRespondErrorMapsKnownErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "invalid period", err: service.ErrInvalidPeriod, wantStatus: 400},
		{name: "invalid id", err: service.ErrInvalidID, wantStatus: 400},
		{name: "forbidden", err: service.ErrForbidden, wantStatus: 403},
		{name: "incident not found", err: repository.ErrIncidentNotFound, wantStatus: 404},
		{name: "internal error", err: errors.New("database failure"), wantStatus: 500},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, response := testContext()
			handler := &Handler{}

			if !handler.respondError(context, test.err) {
				t.Fatal("respondError returned false")
			}
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestRespondErrorDoesNotExposeInternalDetails(t *testing.T) {
	context, response := testContext()
	handler := &Handler{}

	if !handler.respondError(context, errors.New("postgres password or query details")) {
		t.Fatal("respondError returned false")
	}
	if response.Code != 500 {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if response.Body.String() != `{"error":"internal server error"}` {
		t.Fatalf("body = %q, want generic error", response.Body.String())
	}
}

func TestValidPlatformStatus(t *testing.T) {
	for _, status := range []string{"operational", "degraded", "maintenance"} {
		if !validPlatformStatus(status) {
			t.Fatalf("status %q should be valid", status)
		}
	}
	if validPlatformStatus("unknown") {
		t.Fatal("unknown status should be invalid")
	}
}

func TestRespondErrorIgnoresCanceledRequests(t *testing.T) {
	context, response := testContext()
	handler := &Handler{}

	if !handler.respondError(context, contextCanceledError()) {
		t.Fatal("respondError returned false")
	}
	if response.Code != 200 {
		t.Fatalf("status = %d, want no response", response.Code)
	}
}

func testContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	return context, response
}

func contextCanceledError() error {
	return context.Canceled
}

func TestParseIncidentHistoryFilterDefaults(t *testing.T) {
	filter, err := parseIncidentHistoryFilter(url.Values{"search": {"  demoras  "}})
	if err != nil {
		t.Fatal(err)
	}
	if filter.Search != "demoras" || filter.Limit != incidentHistoryDefaultLimit || filter.Offset != 0 || filter.From != nil {
		t.Fatalf("unexpected filter: %+v", filter)
	}
}

func TestParseIncidentHistoryFilterReadsAllParams(t *testing.T) {
	filter, err := parseIncidentHistoryFilter(url.Values{
		"type":    {"maintenance"},
		"service": {"voice"},
		"from":    {"2026-11-01T00:00:00-03:00"},
		"limit":   {"12"},
		"offset":  {"24"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantFrom := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	if filter.Type != "maintenance" || filter.Service != "voice" || filter.Limit != 12 || filter.Offset != 24 ||
		filter.From == nil || !filter.From.Equal(wantFrom) || filter.From.Location() != time.UTC {
		t.Fatalf("unexpected filter: %+v", filter)
	}
}

func TestParseIncidentHistoryFilterRejectsInvalidValues(t *testing.T) {
	for name, values := range map[string]url.Values{
		"operational type": {"type": {"operational"}},
		"unknown type":     {"type": {"broken"}},
		"bad from":         {"from": {"ayer"}},
		"zero limit":       {"limit": {"0"}},
		"huge limit":       {"limit": {"101"}},
		"negative offset":  {"offset": {"-1"}},
		"long search":      {"search": {strings.Repeat("a", incidentHistorySearchMax+1)}},
	} {
		if _, err := parseIncidentHistoryFilter(values); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestIncidentWithoutAuthorsHidesStaffIDs(t *testing.T) {
	staffID := "a2a37e81-1cb1-4fcf-bf6f-e16fca0dd623"
	incident := model.Incident{ID: "1", CreatedBy: &staffID, ResolvedBy: &staffID}
	public := incident.WithoutAuthors()
	if public.CreatedBy != nil || public.ResolvedBy != nil {
		t.Fatalf("authors must be hidden: %+v", public)
	}
	if incident.CreatedBy == nil {
		t.Fatal("the original incident must keep its authors")
	}
}
