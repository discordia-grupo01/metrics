package middleware

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/discordia-grupo01/metrics/internal/apierr"
	"github.com/gin-gonic/gin"
)

const userIDKey = "user_id"

func Auth(userHeader string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := ""
		if userHeader != "" {
			userID = strings.TrimSpace(c.GetHeader(userHeader))
		}
		if userID == "" {
			userID = bearerSubject(c.GetHeader("Authorization"))
		}
		if userID == "" {
			apierr.Unauthorized(c)
			return
		}
		c.Set(userIDKey, userID)
		c.Next()
	}
}

func UserID(c *gin.Context) string {
	value, _ := c.Get(userIDKey)
	userID, _ := value.(string)
	return userID
}

func bearerSubject(header string) string {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	segments := strings.Split(parts[1], ".")
	if len(segments) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return strings.TrimSpace(claims.Sub)
}
