package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGrowthBucketsFromPeriodStartUntilTo(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	serverID := "00000000-0000-0000-0000-000000000001"
	from := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	to := from.Add(36 * time.Hour)
	local := time.FixedZone("ART", -3*60*60)

	mock.ExpectQuery("LEAST\\(start \\+ interval '1 day', \\$3::timestamptz\\)").
		WithArgs(serverID, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"start", "members"}).
			AddRow(from.In(local), 7).
			AddRow(from.Add(24*time.Hour).In(local), 11))

	points, err := New(db).Growth(context.Background(), serverID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}
	if !points[0].Start.Equal(from) || points[0].Start.Location() != time.UTC || points[0].Members != 7 {
		t.Fatalf("unexpected first point: %+v", points[0])
	}
	if points[1].Members != 11 {
		t.Fatalf("unexpected second point: %+v", points[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGrowthReturnsEmptySliceNotNil(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	from := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)

	mock.ExpectQuery("generate_series").
		WillReturnRows(sqlmock.NewRows([]string{"start", "members"}))

	points, err := New(db).Growth(context.Background(), "00000000-0000-0000-0000-000000000001", from, from.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if points == nil {
		t.Fatal("points must serialize as [] instead of null")
	}
}
