package apierr_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/discordia-grupo01/metrics/internal/apierr"
	"github.com/discordia-grupo01/metrics/internal/apierr/apierrtest"
	"github.com/gin-gonic/gin"
)

func newRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	apierr.Register(router)
	router.GET("/ok", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/nil-details", func(c *gin.Context) {
		apierr.Abort(c, http.StatusConflict, "SOME_CONFLICT", "Conflicto.", nil)
	})
	return router
}

func serve(router *gin.Engine, method, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	return response
}

func TestAbortNormalizesNilDetailsToEmptyObject(t *testing.T) {
	response := serve(newRouter(), http.MethodGet, "/nil-details")
	code, details := apierrtest.Envelope(t, response)
	if response.Code != http.StatusConflict || code != "SOME_CONFLICT" || len(details) != 0 {
		t.Fatalf("got %d %s %v", response.Code, code, details)
	}
	if want := `{"error":{"code":"SOME_CONFLICT","message":"Conflicto.","details":{}}}`; response.Body.String() != want {
		t.Fatalf("body = %s, want %s", response.Body.String(), want)
	}
}

func TestUnknownRouteReturnsNotFoundEnvelope(t *testing.T) {
	response := serve(newRouter(), http.MethodGet, "/nope")
	code, _ := apierrtest.Envelope(t, response)
	if response.Code != http.StatusNotFound || code != apierr.CodeNotFound {
		t.Fatalf("got %d %s", response.Code, code)
	}
}

func TestMethodNotAllowedReturnsEnvelope(t *testing.T) {
	response := serve(newRouter(), http.MethodPost, "/ok")
	code, _ := apierrtest.Envelope(t, response)
	if response.Code != http.StatusMethodNotAllowed || code != apierr.CodeMethodNotAllowed {
		t.Fatalf("got %d %s", response.Code, code)
	}
	if allow := response.Header().Get("Allow"); allow != "GET" {
		t.Fatalf("Allow = %q, want GET", allow)
	}
}

func TestWrappersUseCatalogCodes(t *testing.T) {
	tests := []struct {
		name       string
		call       func(*gin.Context)
		wantStatus int
		wantCode   string
		wantDetail string
	}{
		{"invalid query", func(c *gin.Context) { apierr.InvalidQuery(c, "from", "m") }, 400, "INVALID_QUERY", "param"},
		{"invalid parameter", func(c *gin.Context) { apierr.InvalidParameter(c, "id", "m") }, 400, "INVALID_PARAMETER", "param"},
		{"invalid body field", func(c *gin.Context) { apierr.InvalidBody(c, "title", "required", "m") }, 400, "INVALID_BODY", "field"},
		{"unauthorized", apierr.Unauthorized, 401, "UNAUTHORIZED", ""},
		{"forbidden", apierr.Forbidden, 403, "FORBIDDEN", ""},
		{"internal", apierr.Internal, 500, "INTERNAL_ERROR", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			test.call(c)
			code, details := apierrtest.Envelope(t, response)
			if response.Code != test.wantStatus || code != test.wantCode {
				t.Fatalf("got %d %s", response.Code, code)
			}
			if _, ok := details[test.wantDetail]; test.wantDetail != "" && !ok {
				t.Fatalf("details = %v, want key %q", details, test.wantDetail)
			}
		})
	}
}

func TestInvalidBodyWithoutFieldHasEmptyDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	apierr.InvalidBody(c, "", "", "JSON mal formado.")
	if _, details := apierrtest.Envelope(t, response); len(details) != 0 {
		t.Fatalf("details = %v, want {}", details)
	}
}

func TestRegisterNeverRedirects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	apierr.Register(router)
	router.GET("/v1/things/:id", func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, path := range []string{"/v1/things/", "/v1/things/%2F", "/v1//things/1"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code >= 300 && recorder.Code < 400 {
			t.Errorf("GET %s answered %d with Location %q, want no redirect", path, recorder.Code, recorder.Header().Get("Location"))
		}
	}
}
