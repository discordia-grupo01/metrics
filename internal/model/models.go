package model

import "time"

type ServerSummary struct {
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	ActiveMembers int       `json:"active_members"`
	MessagesSent  int       `json:"messages_sent"`
	NewMembers    int       `json:"new_members"`
	HasActivity   bool      `json:"has_activity"`
}

type GlobalSummary struct {
	From           time.Time     `json:"from"`
	To             time.Time     `json:"to"`
	ActiveUsers    int           `json:"active_users"`
	ServersCreated int           `json:"servers_created"`
	MessagesSent   int           `json:"messages_sent"`
	HasActivity    bool          `json:"has_activity"`
	Granularity    string        `json:"granularity"`
	Series         []GlobalPoint `json:"series"`
}

type GlobalPoint struct {
	Start          time.Time `json:"start"`
	ActiveUsers    int       `json:"active_users"`
	ServersCreated int       `json:"servers_created"`
	MessagesSent   int       `json:"messages_sent"`
}

const (
	GranularityHour = "hour"
	GranularityDay  = "day"
)

type GrowthPoint struct {
	Date    string `json:"date"`
	Members int    `json:"members"`
}

type Incident struct {
	ID               string     `json:"id"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	AffectedServices []string   `json:"affected_services"`
	PlatformStatus   string     `json:"platform_status"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
	CreatedBy        *string    `json:"created_by,omitempty"`
	ResolvedBy       *string    `json:"resolved_by,omitempty"`
}

// WithoutAuthors saca quién publicó y quién resolvió: GET /v1/status lo ve
// cualquier usuario y los ids del staff no son para todos.
func (i Incident) WithoutAuthors() Incident {
	i.CreatedBy = nil
	i.ResolvedBy = nil
	return i
}

type IncidentInput struct {
	Title            string   `json:"title" binding:"required"`
	Description      string   `json:"description" binding:"required"`
	AffectedServices []string `json:"affected_services"`
	PlatformStatus   string   `json:"platform_status"`
}

type IncidentHistoryFilter struct {
	Search  string
	Type    string
	Service string
	From    *time.Time
	Limit   int
	Offset  int
}

type IncidentPage struct {
	Incidents []Incident `json:"incidents"`
	Total     int        `json:"total"`
}

const (
	PlatformOperational = "operational"
	PlatformDegraded    = "degraded"
	PlatformMaintenance = "maintenance"
)
