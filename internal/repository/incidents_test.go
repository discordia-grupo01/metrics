package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/discordia-grupo01/metrics/internal/model"
)

func TestResolvedIncidentsAppliesFiltersAndPagination(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	from := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 11, 18, 21, 10, 0, 0, time.UTC)
	resolved := created.Add(55 * time.Minute)
	staffID := "a2a37e81-1cb1-4fcf-bf6f-e16fca0dd623"

	where := `WHERE status = 'resolved' AND \(title ILIKE \$1 OR description ILIKE \$1\) AND platform_status = \$2 AND \$3 = ANY\(affected_services\) AND created_at >= \$4`
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM incidents `+where).
		WithArgs(`%50\%%`, "degraded", "messaging", from).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(13))
	mock.ExpectQuery(where+` ORDER BY created_at DESC, id LIMIT \$5 OFFSET \$6`).
		WithArgs(`%50\%%`, "degraded", "messaging", from, 12, 12).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "description", "affected_services", "platform_status", "status", "created_at", "resolved_at", "created_by", "resolved_by"}).
			AddRow("8f1c0000-0000-0000-0000-000000000001", "Demoras", "Mensajes lentos", "{messaging}", "degraded", "resolved", created, resolved, staffID, nil))

	page, err := New(db).ResolvedIncidents(context.Background(), model.IncidentHistoryFilter{
		Search: "50%", Type: "degraded", Service: "messaging", From: &from, Limit: 12, Offset: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 13 || len(page.Incidents) != 1 {
		t.Fatalf("unexpected page: %+v", page)
	}
	incident := page.Incidents[0]
	if len(incident.AffectedServices) != 1 || incident.AffectedServices[0] != "messaging" || incident.ResolvedAt == nil ||
		incident.CreatedBy == nil || *incident.CreatedBy != staffID || incident.ResolvedBy != nil {
		t.Fatalf("unexpected incident: %+v", incident)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestResolvedIncidentsWithoutFiltersReturnsEmptySlice(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM incidents WHERE status = 'resolved'$`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`WHERE status = 'resolved' ORDER BY created_at DESC, id LIMIT \$1 OFFSET \$2`).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "description", "affected_services", "platform_status", "status", "created_at", "resolved_at", "created_by", "resolved_by"}))

	page, err := New(db).ResolvedIncidents(context.Background(), model.IncidentHistoryFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Incidents == nil || page.Total != 0 {
		t.Fatalf("incidents must serialize as [] instead of null: %+v", page)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
