// Package apierrtest ofrece verificaciones compartidas de la forma del sobre de error.
package apierrtest

import (
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// Envelope verifica que la respuesta sea JSON con la forma completa del sobre
// {"error": {"code", "message", "details"}} y devuelve code y details.
func Envelope(t *testing.T, response *httptest.ResponseRecorder) (string, map[string]any) {
	t.Helper()
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &top); err != nil {
		t.Fatalf("body %q is not a JSON object: %v", response.Body.String(), err)
	}
	if len(top) != 1 || top["error"] == nil {
		t.Fatalf("body %q must have only the error key", response.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(top["error"], &body); err != nil {
		t.Fatalf("error is not an object: %v", err)
	}
	var code, message string
	if err := json.Unmarshal(body["code"], &code); err != nil || !codePattern.MatchString(code) {
		t.Fatalf("code = %s, want SCREAMING_SNAKE_CASE string", body["code"])
	}
	if err := json.Unmarshal(body["message"], &message); err != nil || strings.TrimSpace(message) == "" {
		t.Fatalf("message = %s, want non-empty string", body["message"])
	}
	var details map[string]any
	if raw := string(body["details"]); !strings.HasPrefix(raw, "{") {
		t.Fatalf("details = %s, want object", raw)
	}
	_ = json.Unmarshal(body["details"], &details)
	return code, details
}
