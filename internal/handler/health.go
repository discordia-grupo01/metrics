package handler

import (
	"context"
	"net/http"

	"github.com/discordia-grupo01/metrics/internal/model"
	"github.com/gin-gonic/gin"
)

// Pinger es la dependencia que /readyz verifica (la base de datos).
type Pinger interface {
	PingContext(ctx context.Context) error
}

// Livez confirma que el proceso está vivo. No consulta dependencias.
//
// @Summary      Liveness check
// @Description  Confirma que el proceso está vivo. No consulta dependencias; responde 200 sin cuerpo.
// @Tags         Health
// @Success      200  "El proceso está vivo"
// @Router       /livez [get]
func Livez(c *gin.Context) { c.Status(http.StatusOK) }

// Readyz confirma que el servicio puede hablar con PostgreSQL.
//
// @Summary      Readiness check
// @Description  Verifica que el servicio puede conectarse a PostgreSQL.
// @Tags         Health
// @Produce      json
// @Success      200  {object}  model.StatusResponse  "Listo para recibir tráfico"
// @Failure      503  {object}  model.StatusResponse  "PostgreSQL no responde (status not_ready)"
// @Router       /readyz [get]
func Readyz(db Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := db.PingContext(c); err != nil {
			c.JSON(http.StatusServiceUnavailable, model.StatusResponse{Status: "not_ready"})
			return
		}
		c.JSON(http.StatusOK, model.StatusResponse{Status: "ready"})
	}
}
