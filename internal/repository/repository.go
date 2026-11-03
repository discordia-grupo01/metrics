package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/discordia-grupo01/metrics/internal/model"
	"github.com/google/uuid"
)

var ErrIncidentNotFound = errors.New("incident not found or already resolved")

type Repository struct{ db *sql.DB }

func New(db *sql.DB) *Repository { return &Repository{db: db} }

type Event struct {
	ID          uuid.UUID
	Type        string
	ServerID    *uuid.UUID
	UserID      *uuid.UUID
	RoleID      *uuid.UUID
	RoleIDs     []uuid.UUID
	Permissions []string
	IsEveryone  bool
	OwnerID     *uuid.UUID
	OccurredAt  time.Time
}

func (r *Repository) ApplyEvent(ctx context.Context, event Event) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `INSERT INTO metric_events (event_id,event_type,server_id,user_id,occurred_at)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (event_id) DO NOTHING`,
		event.ID, event.Type, event.ServerID, event.UserID, event.OccurredAt)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return tx.Commit()
	}

	switch event.Type {
	case "server_updated":
		_, err = tx.ExecContext(ctx, `INSERT INTO metric_servers (server_id, occurred_at, deleted_at)
			VALUES ($1,$2,NULL)
			ON CONFLICT (server_id) DO UPDATE SET occurred_at=EXCLUDED.occurred_at, deleted_at=NULL
			WHERE metric_servers.occurred_at <= EXCLUDED.occurred_at`, event.ServerID, event.OccurredAt)
	case "server_deleted":
		_, err = tx.ExecContext(ctx, `INSERT INTO metric_servers (server_id, occurred_at, deleted_at)
			VALUES ($1,$2,$2)
			ON CONFLICT (server_id) DO UPDATE SET occurred_at=EXCLUDED.occurred_at, deleted_at=EXCLUDED.deleted_at
			WHERE metric_servers.occurred_at <= EXCLUDED.occurred_at`, event.ServerID, event.OccurredAt)
	case "role_updated":
		_, err = tx.ExecContext(ctx, `INSERT INTO metric_roles
			(server_id, role_id, is_admin, is_everyone, occurred_at, deleted)
			VALUES ($1,$2,$3,$4,$5,false)
			ON CONFLICT (server_id, role_id) DO UPDATE SET
				is_admin=EXCLUDED.is_admin, is_everyone=EXCLUDED.is_everyone,
				occurred_at=EXCLUDED.occurred_at, deleted=false
			WHERE metric_roles.occurred_at <= EXCLUDED.occurred_at`,
			event.ServerID, event.RoleID, contains(event.Permissions, "ADMINISTRATOR"),
			event.IsEveryone, event.OccurredAt)
	case "role_deleted":
		_, err = tx.ExecContext(ctx, `INSERT INTO metric_roles
			(server_id, role_id, is_admin, is_everyone, occurred_at, deleted)
			VALUES ($1,$2,false,false,$3,true)
			ON CONFLICT (server_id, role_id) DO UPDATE SET occurred_at=EXCLUDED.occurred_at, deleted=true
			WHERE metric_roles.occurred_at <= EXCLUDED.occurred_at`,
			event.ServerID, event.RoleID, event.OccurredAt)
	case "member_roles_updated":
		_, err = tx.ExecContext(ctx, `INSERT INTO metric_member_roles
			(server_id, user_id, role_ids, occurred_at)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (server_id, user_id) DO UPDATE SET role_ids=EXCLUDED.role_ids,
				occurred_at=EXCLUDED.occurred_at
			WHERE metric_member_roles.occurred_at <= EXCLUDED.occurred_at`,
			event.ServerID, event.UserID, uuidStrings(event.RoleIDs), event.OccurredAt)
	case "owner_updated":
		_, err = tx.ExecContext(ctx, `INSERT INTO metric_owners (server_id, owner_id, occurred_at)
			VALUES ($1,$2,$3)
			ON CONFLICT (server_id) DO UPDATE SET owner_id=EXCLUDED.owner_id, occurred_at=EXCLUDED.occurred_at
			WHERE metric_owners.occurred_at <= EXCLUDED.occurred_at`,
			event.ServerID, event.OwnerID, event.OccurredAt)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) CanAdministerServer(ctx context.Context, serverID, userID string) (bool, error) {
	var allowed bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1
		FROM metric_servers s
		WHERE s.server_id = $1 AND s.deleted_at IS NULL
		AND (
			EXISTS (SELECT 1 FROM metric_owners o WHERE o.server_id = s.server_id AND o.owner_id = $2)
			OR EXISTS (
				SELECT 1
				FROM metric_member_roles mr
				JOIN LATERAL unnest(mr.role_ids) assigned(role_id) ON true
				JOIN metric_roles r ON r.server_id = mr.server_id AND r.role_id = assigned.role_id
				WHERE mr.server_id = s.server_id AND mr.user_id = $2
					AND r.is_admin AND NOT r.deleted
			)
		)
	)`, serverID, userID).Scan(&allowed)
	return allowed, err
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = value.String()
	}
	return result
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

func (r *Repository) GlobalSeries(ctx context.Context, from, to time.Time, step time.Duration) ([]model.GlobalPoint, error) {
	rows, err := r.db.QueryContext(ctx, `WITH buckets AS (
		SELECT start, LEAST(start + $3::interval, $2::timestamptz) AS finish
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 microsecond', $3::interval) AS start
	)
	SELECT b.start,
		COUNT(DISTINCT e.user_id) FILTER (WHERE e.event_type IN ('message','member_joined','member_left')),
		COUNT(*) FILTER (WHERE e.event_type = 'message'),
		COUNT(DISTINCT e.server_id) FILTER (WHERE e.event_type = 'server_updated'
			AND NOT EXISTS (SELECT 1 FROM metric_events old WHERE old.server_id = e.server_id
				AND old.event_type = 'server_updated' AND old.occurred_at < e.occurred_at))
	FROM buckets b
	LEFT JOIN metric_events e ON e.occurred_at >= b.start AND e.occurred_at < b.finish
	GROUP BY b.start ORDER BY b.start`, from, to, intervalLiteral(step))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.GlobalPoint, 0)
	for rows.Next() {
		var p model.GlobalPoint
		if err := rows.Scan(&p.Start, &p.ActiveUsers, &p.MessagesSent, &p.ServersCreated); err != nil {
			return nil, err
		}
		p.Start = p.Start.UTC()
		result = append(result, p)
	}
	return result, rows.Err()
}

func intervalLiteral(step time.Duration) string {
	return strconv.FormatInt(int64(step/time.Second), 10) + " seconds"
}

func (r *Repository) Incidents(ctx context.Context, activeOnly bool) ([]model.Incident, error) {
	query := `SELECT id,title,description,affected_services,platform_status,status,created_at,resolved_at FROM incidents`
	if activeOnly {
		query += ` WHERE status = 'active'`
	}
	query += ` ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.Incident, 0)
	for rows.Next() {
		var i model.Incident
		if err := rows.Scan(&i.ID, &i.Title, &i.Description, &i.AffectedServices, &i.PlatformStatus, &i.Status, &i.CreatedAt, &i.ResolvedAt); err != nil {
			return nil, err
		}
		result = append(result, i)
	}
	return result, rows.Err()
}

func (r *Repository) CreateIncident(ctx context.Context, input model.IncidentInput) (model.Incident, error) {
	id := uuid.New()
	var i model.Incident
	err := r.db.QueryRowContext(ctx, `INSERT INTO incidents (id,title,description,affected_services,platform_status)
		VALUES ($1,$2,$3,$4,$5) RETURNING id,title,description,affected_services,platform_status,status,created_at,resolved_at`,
		id, strings.TrimSpace(input.Title), strings.TrimSpace(input.Description), input.AffectedServices, input.PlatformStatus).
		Scan(&i.ID, &i.Title, &i.Description, &i.AffectedServices, &i.PlatformStatus, &i.Status, &i.CreatedAt, &i.ResolvedAt)
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
