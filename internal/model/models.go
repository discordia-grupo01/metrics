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
	From           time.Time `json:"from"`
	To             time.Time `json:"to"`
	ActiveUsers    int       `json:"active_users"`
	ServersCreated int       `json:"servers_created"`
	MessagesSent   int       `json:"messages_sent"`
	HasActivity    bool      `json:"has_activity"`
}

type GrowthPoint struct {
	Date    string `json:"date"`
	Members int    `json:"members"`
}

type Incident struct {
	ID               string     `json:"id"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	AffectedServices []string   `json:"affected_services"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
}

type IncidentInput struct {
	Title            string   `json:"title" binding:"required"`
	Description      string   `json:"description" binding:"required"`
	AffectedServices []string `json:"affected_services"`
}
