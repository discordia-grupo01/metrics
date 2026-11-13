package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
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

func TestPeriodRejectsInvalidQuery(t *testing.T) {
	context, response := testContext()
	context.Request = httptest.NewRequest(http.MethodGet, "/?from=invalid", nil)
	handler := &Handler{}

	_, _, ok := handler.period(context)
	if ok {
		t.Fatal("period accepted an invalid timestamp")
	}
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestPeriodUsesThirtyDayDefault(t *testing.T) {
	now := time.Now().UTC()
	context, _ := testContext()
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	handler := &Handler{}

	from, to, ok := handler.period(context)
	if !ok {
		t.Fatal("period rejected default range")
	}
	if to.Sub(from) != 30*24*time.Hour {
		t.Fatalf("period duration = %s, want 720h", to.Sub(from))
	}
	if to.Before(now.Add(-time.Second)) || to.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("period end = %s, want current time", to)
	}
}

func TestResolveIncidentRejectsInvalidIDBeforeRepository(t *testing.T) {
	context, response := testContext()
	context.Params = gin.Params{{Key: "id", Value: "invalid"}}
	context.Set("authorizer", testAuthorizer{staff: true})
	context.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	handler := &Handler{}

	handler.ResolveIncident(context)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestServiceHealthReturnsEmptySnapshotWithoutMonitor(t *testing.T) {
	context, response := testContext()
	context.Set("authorizer", testAuthorizer{staff: true})
	context.Set("user_id", "staff")
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	handler := &Handler{}

	handler.ServiceHealth(context)
	if response.Code != http.StatusOK || response.Body.String() != `{"services":[]}` {
		t.Fatalf("response = %d %s, want empty service snapshot", response.Code, response.Body.String())
	}
}

func TestStaffEndpointsRejectNonStaff(t *testing.T) {
	tests := []struct {
		name string
		call func(*Handler, *gin.Context)
	}{
		{name: "service health", call: (*Handler).ServiceHealth},
		{name: "global", call: (*Handler).Global},
		{name: "create incident", call: (*Handler).CreateIncident},
		{name: "resolve incident", call: (*Handler).ResolveIncident},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, response := testContext()
			context.Set("authorizer", testAuthorizer{})
			context.Set("user_id", "user")
			context.Params = gin.Params{{Key: "id", Value: uuidForTest()}}
			context.Request = httptest.NewRequest(http.MethodPost, "/", nil)

			test.call(&Handler{}, context)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
		})
	}
}

func TestCreateIncidentRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: "{"},
		{name: "missing title", body: `{"description":"outage"}`},
		{name: "missing description", body: `{"title":"outage"}`},
		{name: "invalid status", body: `{"title":"outage","description":"details","platform_status":"unknown"}`},
		{name: "title too long", body: `{"title":"` + strings.Repeat("x", 161) + `","description":"details"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, response := testContext()
			context.Set("authorizer", testAuthorizer{staff: true})
			context.Set("user_id", "staff")
			context.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			context.Request.Header.Set("Content-Type", "application/json")

			(&Handler{}).CreateIncident(context)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestMetricEndpointsRejectInvalidPeriod(t *testing.T) {
	tests := []struct {
		name string
		call func(*Handler, *gin.Context)
	}{
		{name: "summary", call: (*Handler).ServerSummary},
		{name: "growth", call: (*Handler).ServerGrowth},
		{name: "export", call: (*Handler).Export},
		{name: "global", call: (*Handler).Global},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, response := testContext()
			context.Set("authorizer", testAuthorizer{staff: true})
			context.Set("user_id", "staff")
			context.Params = gin.Params{{Key: "serverId", Value: uuidForTest()}}
			context.Request = httptest.NewRequest(http.MethodGet, "/?from=invalid", nil)

			test.call(&Handler{}, context)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestIncidentsReturnsActiveIncidents(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	mock.ExpectQuery("SELECT id,title,description,affected_services,platform_status,status,created_at,resolved_at").
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "description", "affected_services", "platform_status", "status", "created_at", "resolved_at"}).
			AddRow("incident-id", "title", "description", "{api}", "degraded", "active", now, nil))

	context, response := testContext()
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	(&Handler{repo: repository.New(db)}).Incidents(context)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"degraded"`) {
		t.Fatalf("response = %d %s, want degraded status", response.Code, response.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func uuidForTest() string {
	return "00000000-0000-0000-0000-000000000001"
}

type testAuthorizer struct{ staff bool }

func (a testAuthorizer) IsStaff(context.Context, string) bool { return a.staff }

func testContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	return context, response
}

func contextCanceledError() error {
	return context.Canceled
}
