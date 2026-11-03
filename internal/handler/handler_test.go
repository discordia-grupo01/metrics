package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

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
