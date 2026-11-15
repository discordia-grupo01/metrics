package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/discordia-grupo01/metrics/internal/events"
	"github.com/discordia-grupo01/metrics/internal/health"
	"github.com/discordia-grupo01/metrics/internal/logging"
	"github.com/discordia-grupo01/metrics/internal/middleware"
	"github.com/discordia-grupo01/metrics/internal/model"
	"github.com/discordia-grupo01/metrics/internal/repository"
	"github.com/discordia-grupo01/metrics/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	service   *service.Service
	repo      *repository.Repository
	health    *health.Monitor
	publisher events.Publisher
}

func New(svc *service.Service, repo *repository.Repository, monitor *health.Monitor, publisher events.Publisher) *Handler {
	return &Handler{service: svc, repo: repo, health: monitor, publisher: publisher}
}

// ServiceHealthResponse es el último estado observado de los servicios monitoreados.
type ServiceHealthResponse struct {
	Services []health.ServiceStatus `json:"services"`
}

// ServiceHealth devuelve el último estado observado por el monitor.
//
// @Summary      Consultar el estado de salud de los servicios
// @Description  Requiere staff. Devuelve el último estado observado por el monitor de metrics.
// @Tags         Incidents
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  ServiceHealthResponse  "Estados observados"
// @Failure      401  {object}  model.ErrorResponse  "Falta una credencial válida"
// @Failure      403  {object}  model.ErrorResponse  "El usuario no tiene rol de staff"
// @Router       /v1/health/services [get]
func (h *Handler) ServiceHealth(c *gin.Context) {
	authorizer := c.MustGet("authorizer").(service.Authorizer)
	if !authorizer.IsStaff(c.Request.Context(), middleware.UserID(c)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "staff role required"})
		return
	}
	if h.health == nil {
		c.JSON(http.StatusOK, ServiceHealthResponse{Services: []health.ServiceStatus{}})
		return
	}
	c.JSON(http.StatusOK, ServiceHealthResponse{Services: h.health.Snapshot()})
}

// ServerSummary devuelve el resumen de actividad de un servidor.
//
// @Summary      Consultar el resumen de actividad de un servidor
// @Description  Devuelve miembros activos, mensajes enviados y nuevos miembros dentro del período solicitado. Requiere rol de staff. Si no hubo actividad, responde 200 con contadores en cero y has_activity=false.
// @Tags         Server metrics
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path   string  true   "UUID del servidor"  format(uuid)
// @Param        from      query  string  false  "Inicio inclusivo del período en RFC3339 (por defecto, 30 días antes de `to`)"  format(date-time)
// @Param        to        query  string  false  "Fin exclusivo del período en RFC3339 (por defecto, ahora); el período máximo es de 366 días"  format(date-time)
// @Success      200  {object}  model.ServerSummary  "Resumen de actividad"
// @Failure      400  {object}  model.ErrorResponse  "Servidor o período inválidos"
// @Failure      401  {object}  model.ErrorResponse  "Falta una credencial válida"
// @Failure      403  {object}  model.ErrorResponse  "El usuario no tiene rol de staff"
// @Router       /v1/servers/{serverId}/metrics [get]
func (h *Handler) ServerSummary(c *gin.Context) {
	from, to, ok := h.period(c)
	if !ok {
		return
	}
	summary, err := h.service.ServerSummary(c, c.Param("serverId"), middleware.UserID(c), from, to)
	if h.respondError(c, err) {
		return
	}
	c.JSON(http.StatusOK, summary)
}

// ServerGrowth devuelve la serie diaria de miembros de un servidor.
//
// @Summary      Consultar el crecimiento diario de miembros
// @Description  Devuelve un punto por cada tramo de 24 h desde `from` (el último termina en `to`), con la cantidad de miembros al cierre del tramo. Los tramos sin cambios se incluyen con el total. Requiere rol de staff.
// @Tags         Server metrics
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path   string  true   "UUID del servidor"  format(uuid)
// @Param        from      query  string  false  "Inicio inclusivo del período en RFC3339 (por defecto, 30 días antes de `to`)"  format(date-time)
// @Param        to        query  string  false  "Fin exclusivo del período en RFC3339 (por defecto, ahora); el período máximo es de 366 días"  format(date-time)
// @Success      200  {object}  model.GrowthResponse  "Serie diaria de miembros"
// @Failure      400  {object}  model.ErrorResponse   "Servidor o período inválidos"
// @Failure      401  {object}  model.ErrorResponse  "Falta una credencial válida"
// @Failure      403  {object}  model.ErrorResponse  "El usuario no tiene rol de staff"
// @Router       /v1/servers/{serverId}/metrics/growth [get]
func (h *Handler) ServerGrowth(c *gin.Context) {
	from, to, ok := h.period(c)
	if !ok {
		return
	}
	points, err := h.service.ServerGrowth(c, c.Param("serverId"), middleware.UserID(c), from, to)
	if h.respondError(c, err) {
		return
	}
	c.JSON(http.StatusOK, model.GrowthResponse{From: from, To: to, HasActivity: len(points) > 0, Points: points})
}

// Export devuelve el resumen de un servidor como CSV.
//
// @Summary      Exportar métricas de un servidor a CSV
// @Description  Genera un archivo CSV con el resumen del período. Requiere rol de staff.
// @Tags         Server metrics
// @Produce      json,text/csv
// @Security     BearerAuth
// @Param        serverId  path   string  true   "UUID del servidor"  format(uuid)
// @Param        from      query  string  false  "Inicio inclusivo del período en RFC3339 (por defecto, 30 días antes de `to`)"  format(date-time)
// @Param        to        query  string  false  "Fin exclusivo del período en RFC3339 (por defecto, ahora); el período máximo es de 366 días"  format(date-time)
// @Success      200  {file}    file                 "Archivo CSV generado (text/csv); los errores responden JSON"
// @Header       200  {string}  Content-Disposition  "Nombre sugerido del archivo, attachment; filename=metrics.csv"
// @Failure      400  {object}  model.ErrorResponse  "Servidor o período inválidos"
// @Failure      401  {object}  model.ErrorResponse  "Falta una credencial válida"
// @Failure      403  {object}  model.ErrorResponse  "El usuario no tiene rol de staff"
// @Router       /v1/servers/{serverId}/metrics/export.csv [get]
func (h *Handler) Export(c *gin.Context) {
	from, to, ok := h.period(c)
	if !ok {
		return
	}
	summary, err := h.service.ServerSummary(c, c.Param("serverId"), middleware.UserID(c), from, to)
	if h.respondError(c, err) {
		return
	}
	if err := service.WriteCSV(c.Writer, summary); err != nil {
		c.Error(err)
	}
}

// Global devuelve las métricas agregadas de toda la plataforma.
//
// @Summary      Consultar métricas globales de la plataforma
// @Description  Agrega la actividad de todos los servidores. Requiere que el usuario autenticado esté en la proyección local de staff sincronizada desde admin. Además de los totales devuelve una serie para graficar: los períodos de hasta 24 horas se dividen por hora y los más largos por día. Los tramos empiezan en `from`, así que si el cliente manda la medianoche local los días coinciden con su zona horaria. El último tramo se recorta en `to`.
// @Tags         Platform metrics
// @Produce      json
// @Security     BearerAuth
// @Param        from      query  string  false  "Inicio inclusivo del período en RFC3339 (por defecto, 30 días antes de `to`)"  format(date-time)
// @Param        to        query  string  false  "Fin exclusivo del período en RFC3339 (por defecto, ahora); el período máximo es de 366 días"  format(date-time)
// @Success      200  {object}  model.GlobalSummary  "Resumen global"
// @Failure      400  {object}  model.ErrorResponse  "Período inválido"
// @Failure      401  {object}  model.ErrorResponse  "Falta una credencial válida"
// @Failure      403  {object}  model.ErrorResponse  "El usuario no tiene rol de staff"
// @Router       /v1/metrics/global [get]
func (h *Handler) Global(c *gin.Context) {
	authorizer := c.MustGet("authorizer").(service.Authorizer)
	if !authorizer.IsStaff(c.Request.Context(), middleware.UserID(c)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "staff role required"})
		return
	}
	from, to, ok := h.period(c)
	if !ok {
		return
	}
	summary, err := h.service.Global(c, from, to)
	if h.respondError(c, err) {
		return
	}
	c.JSON(http.StatusOK, summary)
}

// Incidents devuelve el estado actual de la plataforma y sus incidentes activos.
//
// @Summary      Consultar el estado actual de la plataforma
// @Description  Devuelve el estado actual y los incidentes activos para cualquier usuario autenticado.
// @Tags         Incidents
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  model.PlatformStatusResponse  "Estado e incidentes activos"
// @Failure      401  {object}  model.ErrorResponse           "Falta una credencial válida"
// @Router       /v1/status [get]
func (h *Handler) Incidents(c *gin.Context) {
	incidents, err := h.repo.Incidents(c, true)
	if h.respondError(c, err) {
		return
	}
	platformStatus := model.PlatformOperational
	if len(incidents) > 0 {
		platformStatus = incidents[0].PlatformStatus
	}
	c.JSON(http.StatusOK, model.PlatformStatusResponse{Status: platformStatus, Incidents: incidents})
}

// CreateIncident publica un incidente.
//
// @Summary      Publicar un incidente
// @Description  Requiere que el usuario autenticado sea staff. Si no se envía `platform_status`, se usa `degraded`.
// @Tags         Incidents
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  model.IncidentInput  true  "Datos del incidente"
// @Success      201  {object}  model.Incident       "Incidente creado"
// @Failure      400  {object}  model.ErrorResponse  "Cuerpo inválido o fuera de los límites de largo"
// @Failure      401  {object}  model.ErrorResponse  "Falta una credencial válida"
// @Failure      403  {object}  model.ErrorResponse  "El usuario no tiene rol de staff"
// @Router       /v1/metrics/incidents [post]
func (h *Handler) CreateIncident(c *gin.Context) {
	authorizer := c.MustGet("authorizer").(service.Authorizer)
	if !authorizer.IsStaff(c.Request.Context(), middleware.UserID(c)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "staff role required"})
		return
	}
	var input model.IncidentInput
	if c.ShouldBindJSON(&input) != nil ||
		strings.TrimSpace(input.Title) == "" ||
		strings.TrimSpace(input.Description) == "" ||
		len([]rune(strings.TrimSpace(input.Title))) > 160 ||
		len([]rune(strings.TrimSpace(input.Description))) > 5000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title and description are required and within their length limits"})
		return
	}
	if input.PlatformStatus == "" {
		input.PlatformStatus = model.PlatformDegraded
	}
	if !validPlatformStatus(input.PlatformStatus) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "platform_status must be operational, degraded, or maintenance"})
		return
	}
	incident, err := h.repo.CreateIncident(c, input)
	if h.respondError(c, err) {
		return
	}
	h.publishStatusChanged(c, input.PlatformStatus)
	c.JSON(http.StatusCreated, incident)
}

func validPlatformStatus(value string) bool {
	return value == model.PlatformOperational || value == model.PlatformDegraded || value == model.PlatformMaintenance
}

// ResolveIncident resuelve un incidente activo.
//
// @Summary      Resolver un incidente
// @Description  Requiere staff y un incidente actualmente activo.
// @Tags         Incidents
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  string  true  "UUID del incidente"  format(uuid)
// @Success      204  "Incidente resuelto"
// @Failure      400  {object}  model.ErrorResponse  "Identificador inválido"
// @Failure      401  {object}  model.ErrorResponse  "Falta una credencial válida"
// @Failure      403  {object}  model.ErrorResponse  "El usuario no tiene rol de staff"
// @Failure      404  {object}  model.ErrorResponse  "El incidente no existe o ya estaba resuelto"
// @Router       /v1/metrics/incidents/{id}/resolve [post]
func (h *Handler) ResolveIncident(c *gin.Context) {
	authorizer := c.MustGet("authorizer").(service.Authorizer)
	if !authorizer.IsStaff(c.Request.Context(), middleware.UserID(c)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "staff role required"})
		return
	}
	if _, err := uuid.Parse(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid incident id"})
		return
	}
	if h.respondError(c, h.repo.ResolveIncident(c, c.Param("id"))) {
		return
	}
	status := model.PlatformOperational
	incidents, err := h.repo.Incidents(c, true)
	if err != nil {
		logging.Errorf("platform status lookup after resolving incident failed: %v", err)
		c.Error(err)
	} else if len(incidents) > 0 {
		status = incidents[0].PlatformStatus
	}
	if err == nil {
		h.publishStatusChanged(c, status)
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) publishStatusChanged(c *gin.Context, status string) {
	if h.publisher == nil {
		return
	}
	if err := h.publisher.PublishStatusChanged(c.Request.Context(), status, time.Now().UTC()); err != nil {
		logging.Errorf("platform status notification failed: %v", err)
		c.Error(err)
	}
}

func (h *Handler) period(c *gin.Context) (from, to time.Time, ok bool) {
	from, to, err := service.ParsePeriod(c.Request.URL.Query(), time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return from, to, false
	}
	return from, to, true
}

func (h *Handler) respondError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	switch {
	case errors.Is(err, service.ErrInvalidID):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrInvalidPeriod):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "staff role required"})
	case errors.Is(err, repository.ErrIncidentNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
	return true
}
