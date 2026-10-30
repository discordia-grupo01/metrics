package middleware

import (
	"net/http/httptest"
	"testing"

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
}
