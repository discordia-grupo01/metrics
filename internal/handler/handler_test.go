package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/discordia-grupo01/metrics/internal/apierr"
	"github.com/discordia-grupo01/metrics/internal/apierr/apierrtest"
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
		wantCode   string
	}{
		{name: "invalid period", err: service.ErrInvalidPeriod, wantStatus: 400, wantCode: "INVALID_QUERY"},
		{name: "invalid id", err: service.ErrInvalidID, wantStatus: 400, wantCode: "INVALID_PARAMETER"},
		{name: "forbidden", err: service.ErrForbidden, wantStatus: 403, wantCode: "FORBIDDEN"},
		{name: "incident not found", err: repository.ErrIncidentNotFound, wantStatus: 404, wantCode: "INCIDENT_NOT_FOUND"},
		{name: "internal error", err: errors.New("database failure"), wantStatus: 500, wantCode: "INTERNAL_ERROR"},
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
			if code, _ := apierrtest.Envelope(t, response); code != test.wantCode {
				t.Fatalf("code = %q, want %q", code, test.wantCode)
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
	if strings.Contains(response.Body.String(), "postgres") {
		t.Fatalf("body = %q leaks the internal error", response.Body.String())
	}
	if code, details := apierrtest.Envelope(t, response); code != "INTERNAL_ERROR" || len(details) != 0 {
		t.Fatalf("code = %q details = %v, want INTERNAL_ERROR with empty details", code, details)
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
	if response.Code != apierr.StatusClientClosedRequest {
		t.Fatalf("status = %d, want %d", response.Code, apierr.StatusClientClosedRequest)
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
	code, details := apierrtest.Envelope(t, response)
	if code != "INVALID_QUERY" || details["param"] != "from" {
		t.Fatalf("code = %q details = %v, want INVALID_QUERY on from", code, details)
	}
	if strings.Contains(response.Body.String(), "parsing time") {
		t.Fatalf("body = %q leaks the parser error", response.Body.String())
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
	code, details := apierrtest.Envelope(t, response)
	if code != "INVALID_PARAMETER" || details["param"] != "id" {
		t.Fatalf("code = %q details = %v, want INVALID_PARAMETER on id", code, details)
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
			if code, details := apierrtest.Envelope(t, response); code != "FORBIDDEN" || len(details) != 0 {
				t.Fatalf("code = %q details = %v, want FORBIDDEN with empty details", code, details)
			}
		})
	}
}

func TestCreateIncidentRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantField string
	}{
		{name: "malformed json", body: "{"},
		{name: "missing title", body: `{"description":"outage"}`, wantField: "title"},
		{name: "missing description", body: `{"title":"outage"}`, wantField: "description"},
		{name: "invalid status", body: `{"title":"outage","description":"details","platform_status":"unknown"}`, wantField: "platform_status"},
		{name: "nul in title", body: `{"title":"out\u0000age","description":"details"}`, wantField: "title"},
		{name: "nul in description", body: `{"title":"outage","description":"\u0000"}`, wantField: "description"},
		{name: "nul in service", body: `{"title":"outage","description":"details","affected_services":["\u0000"]}`, wantField: "affected_services"},
		{name: "title too long", body: `{"title":"` + strings.Repeat("x", 161) + `","description":"details"}`, wantField: "title"},
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
			code, details := apierrtest.Envelope(t, response)
			if code != "INVALID_BODY" {
				t.Fatalf("code = %q, want INVALID_BODY", code)
			}
			if test.wantField == "" && len(details) != 0 {
				t.Fatalf("details = %v, want {} for malformed JSON", details)
			}
			if test.wantField != "" && details["field"] != test.wantField {
				t.Fatalf("details = %v, want field %q", details, test.wantField)
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
			if code, details := apierrtest.Envelope(t, response); code != "INVALID_QUERY" || details["param"] != "from" {
				t.Fatalf("code = %q details = %v, want INVALID_QUERY on from", code, details)
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
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "description", "affected_services", "platform_status", "status", "created_at", "resolved_at", "created_by", "resolved_by"}).
			AddRow("incident-id", "title", "description", "{api}", "degraded", "active", now, nil, "a2a37e81-1cb1-4fcf-bf6f-e16fca0dd623", nil))

	context, response := testContext()
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	(&Handler{repo: repository.New(db)}).Incidents(context)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"degraded"`) {
		t.Fatalf("response = %d %s, want degraded status", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "created_by") {
		t.Fatalf("GET /v1/status must not expose staff ids: %s", response.Body.String())
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
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
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
	for name, tc := range map[string]struct {
		values    url.Values
		wantParam string
	}{
		"operational type": {url.Values{"type": {"operational"}}, "type"},
		"unknown type":     {url.Values{"type": {"broken"}}, "type"},
		"bad from":         {url.Values{"from": {"ayer"}}, "from"},
		"zero limit":       {url.Values{"limit": {"0"}}, "limit"},
		"huge limit":       {url.Values{"limit": {"101"}}, "limit"},
		"negative offset":  {url.Values{"offset": {"-1"}}, "offset"},
		"long search":      {url.Values{"search": {strings.Repeat("a", incidentHistorySearchMax+1)}}, "search"},
		"nul in search":    {url.Values{"search": {"a\x00b"}}, "search"},
		"bad utf8 search":  {url.Values{"search": {"\xff"}}, "search"},
		"nul in service":   {url.Values{"service": {"\x00"}}, "service"},
		"bad utf8 service": {url.Values{"service": {"\xff\xfe"}}, "service"},
	} {
		_, invalid := parseIncidentHistoryFilter(tc.values)
		if invalid == nil || invalid.param != tc.wantParam {
			t.Fatalf("%s: invalid = %+v, want param %q", name, invalid, tc.wantParam)
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

func TestExportRejectsInvalidPeriodWithEnvelopeInsteadOfCSV(t *testing.T) {
	context, response := testContext()
	context.Set("authorizer", testAuthorizer{staff: true})
	context.Params = gin.Params{{Key: "serverId", Value: uuidForTest()}}
	context.Request = httptest.NewRequest(http.MethodGet, "/?to=invalid", nil)

	(&Handler{}).Export(context)
	if code, details := apierrtest.Envelope(t, response); code != "INVALID_QUERY" || details["param"] != "to" {
		t.Fatalf("code = %q details = %v, want INVALID_QUERY on to", code, details)
	}
}
