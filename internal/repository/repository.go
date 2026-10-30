package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/discordia-grupo01/metrics/internal/model"
	"github.com/google/uuid"
)

var ErrIncidentNotFound = errors.New("incident not found or already resolved")

type Repository struct{ db *sql.DB }

func New(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) RecordEvent(ctx context.Context, id uuid.UUID, eventType string, serverID, userID *uuid.UUID, occurredAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO metric_events (event_id,event_type,server_id,user_id,occurred_at)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (event_id) DO NOTHING`, id, eventType, serverID, userID, occurredAt)
	return err
}

func (r *Repository) Summary(ctx context.Context, serverID string, from, to time.Time) (model.ServerSummary, error) {
	var s model.ServerSummary
	s.From, s.To = from, to
	err := r.db.QueryRowContext(ctx, `SELECT
		COUNT(DISTINCT user_id) FILTER (WHERE event_type IN ('message','member_joined','member_left')),
		COUNT(*) FILTER (WHERE event_type = 'message'),
		COUNT(*) FILTER (WHERE event_type = 'member_joined')
		FROM metric_events WHERE server_id = $1 AND occurred_at >= $2 AND occurred_at < $3`,
		serverID, from, to).Scan(&s.ActiveMembers, &s.MessagesSent, &s.NewMembers)
	s.HasActivity = s.ActiveMembers > 0 || s.MessagesSent > 0 || s.NewMembers > 0
	return s, err
}

func (r *Repository) Growth(ctx context.Context, serverID string, from, to time.Time) ([]model.GrowthPoint, error) {
	rows, err := r.db.QueryContext(ctx, `WITH days AS (
		SELECT generate_series($2::date, ($3::date - interval '1 day')::date, interval '1 day')::date AS day
	), changes AS (
		SELECT occurred_at::date AS day,
			SUM(CASE WHEN event_type = 'member_joined' THEN 1 WHEN event_type = 'member_left' THEN -1 ELSE 0 END) AS delta
		FROM metric_events WHERE server_id = $1 AND event_type IN ('member_joined','member_left')
		GROUP BY occurred_at::date
	), initial AS (
		SELECT COUNT(*) AS members FROM (
			SELECT user_id, MAX(occurred_at) FILTER (WHERE event_type IN ('member_joined','member_left')) AS last_event,
				(MAX(occurred_at) FILTER (WHERE event_type = 'member_joined')) AS last_join
			FROM metric_events WHERE server_id = $1 AND occurred_at < $2 GROUP BY user_id
		) x WHERE last_join IS NOT NULL AND (last_event IS NULL OR last_join > last_event)
	)
	SELECT d.day, (SELECT members FROM initial) + COALESCE(SUM(c.delta) OVER (ORDER BY d.day), 0)
	FROM days d LEFT JOIN changes c ON c.day = d.day ORDER BY d.day`, serverID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.GrowthPoint
	for rows.Next() {
		var date time.Time
		var members int
		if err := rows.Scan(&date, &members); err != nil {
			return nil, err
		}
		result = append(result, model.GrowthPoint{Date: date.Format("2006-01-02"), Members: members})
	}
	return result, rows.Err()
}

func (r *Repository) Global(ctx context.Context, from, to time.Time) (model.GlobalSummary, error) {
	var s model.GlobalSummary
	s.From, s.To = from, to
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT user_id) FILTER (WHERE event_type IN ('message','member_joined','member_left')),
		COUNT(*) FILTER (WHERE event_type = 'message'),
		COUNT(DISTINCT server_id) FILTER (WHERE event_type = 'server_updated'
			AND NOT EXISTS (SELECT 1 FROM metric_events old WHERE old.server_id = metric_events.server_id
				AND old.event_type = 'server_updated' AND old.occurred_at < metric_events.occurred_at))
		FROM metric_events WHERE occurred_at >= $1 AND occurred_at < $2`, from, to).
		Scan(&s.ActiveUsers, &s.MessagesSent, &s.ServersCreated)
	s.HasActivity = s.ActiveUsers > 0 || s.MessagesSent > 0 || s.ServersCreated > 0
	return s, err
}

func (r *Repository) Incidents(ctx context.Context, activeOnly bool) ([]model.Incident, error) {
	query := `SELECT id,title,description,affected_services,status,created_at,resolved_at FROM incidents`
	if activeOnly {
		query += ` WHERE status = 'active'`
	}
	query += ` ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.Incident
	for rows.Next() {
		var i model.Incident
		if err := rows.Scan(&i.ID, &i.Title, &i.Description, &i.AffectedServices, &i.Status, &i.CreatedAt, &i.ResolvedAt); err != nil {
			return nil, err
		}
		result = append(result, i)
	}
	return result, rows.Err()
}

func (r *Repository) CreateIncident(ctx context.Context, input model.IncidentInput) (model.Incident, error) {
	id := uuid.New()
	var i model.Incident
	err := r.db.QueryRowContext(ctx, `INSERT INTO incidents (id,title,description,affected_services)
		VALUES ($1,$2,$3,$4) RETURNING id,title,description,affected_services,status,created_at,resolved_at`,
		id, strings.TrimSpace(input.Title), strings.TrimSpace(input.Description), input.AffectedServices).
		Scan(&i.ID, &i.Title, &i.Description, &i.AffectedServices, &i.Status, &i.CreatedAt, &i.ResolvedAt)
	return i, err
}

func (r *Repository) ResolveIncident(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE incidents SET status='resolved',resolved_at=now() WHERE id=$1 AND status='active'`, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrIncidentNotFound
	}
	return err
}
