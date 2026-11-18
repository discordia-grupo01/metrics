package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/discordia-grupo01/metrics/internal/apierr/apierrtest"
	"github.com/gin-gonic/gin"
)

func TestAuthReadsSubjectFromBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Auth(""))
	router.GET("/", func(c *gin.Context) { c.String(200, UserID(c)) })
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Authorization", "Bearer eyJhbGciOiJub25lIn0.eyJzdWIiOiJ1c2VyLTEifQ.")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 || response.Body.String() != "user-1" {
		t.Fatalf("unexpected response: %d %q", response.Code, response.Body.String())
	}
}

func TestAuthRejectsMissingCredential(t *testing.T) {
	router := gin.New()
	router.Use(Auth(""))
	router.GET("/", func(c *gin.Context) { t.Fatal("handler should not run") })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != 401 {
		t.Fatalf("status = %d", response.Code)
	}
	if code, details := apierrtest.Envelope(t, response); code != "UNAUTHORIZED" || len(details) != 0 {
		t.Fatalf("code = %q details = %v, want UNAUTHORIZED with empty details", code, details)
	}
}

func TestAuthRejectsMalformedToken(t *testing.T) {
	router := gin.New()
	router.Use(Auth(""))
	router.GET("/", func(c *gin.Context) { t.Fatal("handler should not run") })
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Authorization", "Bearer not-a-jwt")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if code, _ := apierrtest.Envelope(t, response); response.Code != 401 || code != "UNAUTHORIZED" {
		t.Fatalf("got %d %q, want 401 UNAUTHORIZED", response.Code, code)
	}
}
