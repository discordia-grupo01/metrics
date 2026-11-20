package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/discordia-grupo01/metrics/internal/apierr"
	"github.com/discordia-grupo01/metrics/internal/events"
	"github.com/discordia-grupo01/metrics/internal/health"
	"github.com/discordia-grupo01/metrics/internal/logging"
	"github.com/discordia-grupo01/metrics/internal/middleware"
	"github.com/discordia-grupo01/metrics/internal/model"
	"github.com/discordia-grupo01/metrics/internal/repository"
	"github.com/discordia-grupo01/metrics/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
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
		apierr.Forbidden(c)
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
// @Failure      500  {object}  model.ErrorResponse  "Error interno del servidor"
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
// @Failure      500  {object}  model.ErrorResponse  "Error interno del servidor"
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
// @Failure      500  {object}  model.ErrorResponse  "Error interno del servidor"
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
		logging.Errorf("csv export failed: %v", err)
		_ = c.Error(err)
		if !c.Writer.Written() {
			apierr.Internal(c)
		}
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
// @Failure      500  {object}  model.ErrorResponse  "Error interno del servidor"
// @Router       /v1/metrics/global [get]
func (h *Handler) Global(c *gin.Context) {
	authorizer := c.MustGet("authorizer").(service.Authorizer)
	if !authorizer.IsStaff(c.Request.Context(), middleware.UserID(c)) {
		apierr.Forbidden(c)
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
// @Failure      500  {object}  model.ErrorResponse  "Error interno del servidor"
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
	for index := range incidents {
		incidents[index] = incidents[index].WithoutAuthors()
	}
	c.JSON(http.StatusOK, model.PlatformStatusResponse{Status: platformStatus, Incidents: incidents})
}

const (
	incidentHistoryDefaultLimit = 20
	incidentHistoryMaxLimit     = 100
	incidentHistorySearchMax    = 100
)

// IncidentHistory lista los incidentes resueltos.
//
// @Summary      Listar el historial de incidentes resueltos
// @Description  Requiere staff. Devuelve los incidentes resueltos del más nuevo al más viejo, paginados. `created_by` y `resolved_by` son los ids del staff que lo publicó y lo resolvió; faltan en los incidentes anteriores a guardar ese dato.
// @Tags         Incidents
// @Produce      json
// @Security     BearerAuth
// @Param        search   query  string  false  "Busca en el título y la descripción (hasta 100 caracteres)"  maxlength(100)
// @Param        type     query  string  false  "Tipo de incidente"  Enums(degraded, maintenance)
// @Param        service  query  string  false  "Id de un servicio afectado"
// @Param        from     query  string  false  "Solo incidentes creados desde esta fecha (RFC3339)"  format(date-time)
// @Param        limit    query  int     false  "Tamaño de página"  minimum(1)  maximum(100)  default(20)
// @Param        offset   query  int     false  "Cantidad de incidentes a saltear"  minimum(0)  default(0)
// @Success      200  {object}  model.IncidentPage   "Página del historial"
// @Failure      400  {object}  model.ErrorResponse  "Algún parámetro de la query es inválido"
// @Failure      401  {object}  model.ErrorResponse  "Falta una credencial válida"
// @Failure      403  {object}  model.ErrorResponse  "El usuario no tiene rol de staff"
// @Failure      500  {object}  model.ErrorResponse  "Error interno del servidor"
// @Router       /v1/metrics/incidents [get]
func (h *Handler) IncidentHistory(c *gin.Context) {
	authorizer := c.MustGet("authorizer").(service.Authorizer)
	if !authorizer.IsStaff(c.Request.Context(), middleware.UserID(c)) {
		apierr.Forbidden(c)
		return
	}
	filter, invalid := parseIncidentHistoryFilter(c.Request.URL.Query())
	if invalid != nil {
		apierr.InvalidQuery(c, invalid.param, invalid.message)
		return
	}
	page, err := h.repo.ResolvedIncidents(c, filter)
	if h.respondError(c, err) {
		return
	}
	c.JSON(http.StatusOK, page)
}

// isStorableText descarta lo que Postgres no acepta en un texto (UTF-8
// inválido o el byte NUL): llegaría como error 500 en vez de un 400.
func isStorableText(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

// unstorableIncidentField devuelve el primer campo de texto que Postgres no
// podría guardar.
func unstorableIncidentField(input model.IncidentInput) (string, bool) {
	if !isStorableText(input.Title) {
		return "title", true
	}
	if !isStorableText(input.Description) {
		return "description", true
	}
	for _, service := range input.AffectedServices {
		if !isStorableText(service) {
			return "affected_services", true
		}
	}
	return "", false
}

type invalidHistoryParam struct {
	param   string
	message string
}

func parseIncidentHistoryFilter(values url.Values) (model.IncidentHistoryFilter, *invalidHistoryParam) {
	filter := model.IncidentHistoryFilter{
		Search:  strings.TrimSpace(values.Get("search")),
		Type:    values.Get("type"),
		Service: values.Get("service"),
		Limit:   incidentHistoryDefaultLimit,
	}
	if !isStorableText(filter.Search) {
		return filter, &invalidHistoryParam{"search", "La búsqueda tiene caracteres inválidos."}
	}
	if len([]rune(filter.Search)) > incidentHistorySearchMax {
		return filter, &invalidHistoryParam{"search", "La búsqueda no puede superar los 100 caracteres."}
	}
	if !isStorableText(filter.Service) {
		return filter, &invalidHistoryParam{"service", "El servicio tiene caracteres inválidos."}
	}
	if filter.Type != "" && filter.Type != model.PlatformDegraded && filter.Type != model.PlatformMaintenance {
		return filter, &invalidHistoryParam{"type", "El tipo debe ser degraded o maintenance."}
	}
	if value := values.Get("from"); value != "" {
		from, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return filter, &invalidHistoryParam{"from", "from debe ser una fecha RFC3339."}
		}
		from = from.UTC()
		filter.From = &from
	}
	if value := values.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > incidentHistoryMaxLimit {
			return filter, &invalidHistoryParam{"limit", "limit debe ser un entero entre 1 y 100."}
		}
		filter.Limit = limit
	}
	if value := values.Get("offset"); value != "" {
		offset, err := strconv.Atoi(value)
		if err != nil || offset < 0 {
			return filter, &invalidHistoryParam{"offset", "offset debe ser un entero mayor o igual a 0."}
		}
		filter.Offset = offset
	}
	return filter, nil
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
// @Failure      500  {object}  model.ErrorResponse  "Error interno del servidor"
// @Router       /v1/metrics/incidents [post]
func (h *Handler) CreateIncident(c *gin.Context) {
	authorizer := c.MustGet("authorizer").(service.Authorizer)
	if !authorizer.IsStaff(c.Request.Context(), middleware.UserID(c)) {
		apierr.Forbidden(c)
		return
	}
	var input model.IncidentInput
	bindErr := c.ShouldBindJSON(&input)
	if bindErr != nil ||
		strings.TrimSpace(input.Title) == "" ||
		strings.TrimSpace(input.Description) == "" ||
		len([]rune(strings.TrimSpace(input.Title))) > 160 ||
		len([]rune(strings.TrimSpace(input.Description))) > 5000 {
		invalidIncident(c, input, bindErr)
		return
	}
	if field, ok := unstorableIncidentField(input); ok {
		apierr.InvalidBody(c, field, "invalid_characters", "El campo tiene caracteres inválidos.")
		return
	}
	if input.PlatformStatus == "" {
		input.PlatformStatus = model.PlatformDegraded
	}
	if !validPlatformStatus(input.PlatformStatus) {
		apierr.InvalidBody(c, "platform_status", "invalid_value", "El estado de la plataforma debe ser operational, degraded o maintenance.")
		return
	}
	incident, err := h.repo.CreateIncident(c, input, middleware.UserID(c))
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
// @Failure      500  {object}  model.ErrorResponse  "Error interno del servidor"
// @Router       /v1/metrics/incidents/{id}/resolve [post]
func (h *Handler) ResolveIncident(c *gin.Context) {
	authorizer := c.MustGet("authorizer").(service.Authorizer)
	if !authorizer.IsStaff(c.Request.Context(), middleware.UserID(c)) {
		apierr.Forbidden(c)
		return
	}
	if _, err := uuid.Parse(c.Param("id")); err != nil {
		apierr.InvalidParameter(c, "id", "El identificador del incidente no es un UUID válido.")
		return
	}
	if h.respondError(c, h.repo.ResolveIncident(c, c.Param("id"), middleware.UserID(c))) {
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
	query := c.Request.URL.Query()
	from, to, err := service.ParsePeriod(query, time.Now())
	if err != nil {
		invalidPeriod(c, query)
		return from, to, false
	}
	return from, to, true
}

func invalidPeriod(c *gin.Context, query url.Values) {
	apierr.InvalidQuery(c, periodParam(query),
		"El período es inválido: from y to deben ser fechas RFC3339, from anterior a to, con un máximo de 366 días.")
}

// periodParam indica el parámetro del período al que se atribuye el error:
// from si no parsea como RFC3339 y, en cualquier otro caso, to.
func periodParam(query url.Values) string {
	if value := query.Get("from"); value != "" {
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return "from"
		}
	}
	return "to"
}

// invalidIncident responde 400 INVALID_BODY con el primer campo inválido del incidente.
func invalidIncident(c *gin.Context, input model.IncidentInput, bindErr error) {
	title := strings.TrimSpace(input.Title)
	description := strings.TrimSpace(input.Description)
	var validation validator.ValidationErrors
	switch {
	case bindErr != nil && !errors.As(bindErr, &validation):
		apierr.InvalidBody(c, "", "", "El cuerpo de la solicitud no es un JSON válido.")
	case title == "":
		apierr.InvalidBody(c, "title", "required", "El título es obligatorio.")
	case len([]rune(title)) > 160:
		apierr.InvalidBody(c, "title", "too_long", "El título no puede superar los 160 caracteres.")
	case description == "":
		apierr.InvalidBody(c, "description", "required", "La descripción es obligatoria.")
	default:
		apierr.InvalidBody(c, "description", "too_long", "La descripción no puede superar los 5000 caracteres.")
	}
}

func (h *Handler) respondError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		// El cliente cortó la conexión: nadie lee el body, pero no se deja un 200 implícito.
		c.AbortWithStatus(apierr.StatusClientClosedRequest)
		return true
	}
	switch {
	case errors.Is(err, service.ErrInvalidID):
		apierr.InvalidParameter(c, "serverId", "El identificador del servidor no es un UUID válido.")
	case errors.Is(err, service.ErrInvalidPeriod):
		invalidPeriod(c, c.Request.URL.Query())
	case errors.Is(err, service.ErrForbidden):
		apierr.Forbidden(c)
	case errors.Is(err, repository.ErrIncidentNotFound):
		apierr.Abort(c, http.StatusNotFound, apierr.CodeIncidentNotFound, "El incidente no existe o ya fue resuelto.", nil)
	default:
		slog.ErrorContext(c.Request.Context(), "internal_error",
			"route", c.FullPath(),
			"error", err.Error(),
		)
		apierr.Internal(c)
	}
	return true
}
