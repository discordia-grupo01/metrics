package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGlobalSeriesBucketsFromPeriodStart(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	from := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	to := from.Add(48 * time.Hour)

	mock.ExpectQuery("generate_series").
		WithArgs(from, to, "86400 seconds").
		WillReturnRows(sqlmock.NewRows([]string{"start", "active_users", "messages_sent", "servers_created"}).
			AddRow(from, 2, 10, 1).
			AddRow(from.Add(24*time.Hour), 0, 0, 0))

	points, err := New(db).GlobalSeries(context.Background(), from, to, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}
	first := points[0]
	if !first.Start.Equal(from) || first.ActiveUsers != 2 || first.MessagesSent != 10 || first.ServersCreated != 1 {
		t.Fatalf("unexpected first point: %+v", first)
	}
	if points[1].ActiveUsers != 0 || points[1].MessagesSent != 0 {
		t.Fatalf("empty bucket should be zero: %+v", points[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalSeriesReturnsEmptySliceNotNil(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	from := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)

	mock.ExpectQuery("generate_series").
		WillReturnRows(sqlmock.NewRows([]string{"start", "active_users", "messages_sent", "servers_created"}))

	points, err := New(db).GlobalSeries(context.Background(), from, from.Add(time.Hour), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if points == nil {
		t.Fatal("series must serialize as [] instead of null")
	}
}
