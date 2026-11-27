package model

import (
	"time"

	"github.com/discordia-grupo01/metrics/internal/apierr"
)

type ServerSummary struct {
	From time.Time `json:"from" format:"date-time"`
	To   time.Time `json:"to" format:"date-time"`
	// ActiveMembers son los usuarios distintos con actividad registrada en el período.
	ActiveMembers int `json:"active_members" minimum:"0"`
	MessagesSent  int `json:"messages_sent" minimum:"0"`
	NewMembers    int `json:"new_members" minimum:"0"`
	// HasActivity es false cuando todos los contadores del período son cero.
	HasActivity bool `json:"has_activity"`
}

type GlobalSummary struct {
	From time.Time `json:"from" format:"date-time"`
	To   time.Time `json:"to" format:"date-time"`
	// ActiveUsers son los usuarios distintos con mensajes o cambios de membresía en el período.
	ActiveUsers int `json:"active_users" minimum:"0"`
	// ServersCreated son los servidores cuyo primer evento server_updated ocurrió en el período.
	ServersCreated int `json:"servers_created" minimum:"0"`
	MessagesSent   int `json:"messages_sent" minimum:"0"`
	// HasActivity es false cuando todos los contadores del período son cero.
	HasActivity bool `json:"has_activity"`
	// Granularity es hour si el período dura hasta 24 horas y day si es más largo.
	Granularity string        `json:"granularity" enums:"hour,day"`
	Series      []GlobalPoint `json:"series"`
}

type GlobalPoint struct {
	// Start es el inicio del tramo, en UTC. El cliente lo muestra en su zona horaria.
	Start time.Time `json:"start" format:"date-time"`
	// ActiveUsers cuenta usuarios distintos dentro del tramo, con el mismo criterio
	// que el total del período. No sumar los tramos para obtener el total: quien
	// estuvo activo varios días aparece en cada uno, pero en el total cuenta una vez.
	ActiveUsers    int `json:"active_users" minimum:"0"`
	ServersCreated int `json:"servers_created" minimum:"0"`
	MessagesSent   int `json:"messages_sent" minimum:"0"`
}

const (
	GranularityHour = "hour"
	GranularityDay  = "day"
)

type GrowthPoint struct {
	// Start es el inicio del tramo, en UTC. El cliente lo muestra en su zona horaria.
	Start   time.Time `json:"start" format:"date-time"`
	Members int       `json:"members" minimum:"0"`
}

// GrowthResponse es la serie de crecimiento de miembros de un servidor.
type GrowthResponse struct {
	From        time.Time     `json:"from" format:"date-time"`
	To          time.Time     `json:"to" format:"date-time"`
	HasActivity bool          `json:"has_activity"`
	Points      []GrowthPoint `json:"points"`
}

type Incident struct {
	ID               string     `json:"id" format:"uuid"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	AffectedServices []string   `json:"affected_services"`
	PlatformStatus   string     `json:"platform_status" enums:"operational,degraded,maintenance"`
	Status           string     `json:"status" enums:"active,resolved"`
	CreatedAt        time.Time  `json:"created_at" format:"date-time"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty" format:"date-time" validate:"optional"`
	// CreatedBy y ResolvedBy son ids de staff. Faltan en los incidentes
	// anteriores a guardar este dato y nunca salen en GET /v1/status.
	CreatedBy  *string `json:"created_by,omitempty" format:"uuid" validate:"optional"`
	ResolvedBy *string `json:"resolved_by,omitempty" format:"uuid" validate:"optional"`
}

// WithoutAuthors saca quién publicó y quién resolvió: GET /v1/status lo ve
// cualquier usuario y los ids del staff no son para todos.
func (i Incident) WithoutAuthors() Incident {
	i.CreatedBy = nil
	i.ResolvedBy = nil
	return i
}

// IncidentInput: title y description se recortan y se rechazan si quedan
// vacíos. swag no emite pattern; scripts/openapi-postprocess.mjs agrega \S.
type IncidentInput struct {
	Title            string   `json:"title" binding:"required" minLength:"1" maxLength:"160"`
	Description      string   `json:"description" binding:"required" minLength:"1" maxLength:"5000"`
	AffectedServices []string `json:"affected_services" example:"metrics,gateway" validate:"optional" extensions:"x-nullable"`
	// PlatformStatus es degraded si se omite; enviado vacío o null es inválido.
	PlatformStatus string `json:"platform_status" enums:"operational,degraded,maintenance" default:"degraded" validate:"optional"`
}

type IncidentHistoryFilter struct {
	Search  string
	Type    string
	Service string
	From    *time.Time
	Limit   int
	Offset  int
}

// IncidentPage es una página del historial de incidentes resueltos.
type IncidentPage struct {
	Incidents []Incident `json:"incidents"`
	Total     int        `json:"total"`
}

const (
	PlatformOperational = "operational"
	PlatformDegraded    = "degraded"
	PlatformMaintenance = "maintenance"
)

// PlatformStatusResponse es el estado actual de la plataforma con sus incidentes activos.
type PlatformStatusResponse struct {
	Status    string     `json:"status" enums:"operational,degraded,maintenance"`
	Incidents []Incident `json:"incidents"`
}

// ErrorResponse es el cuerpo de todos los errores del servicio:
// {"error": {"code", "message", "details"}}.
type ErrorResponse = apierr.ErrorResponse

// StatusResponse es el cuerpo de /readyz.
type StatusResponse struct {
	Status string `json:"status" example:"ready"`
}
