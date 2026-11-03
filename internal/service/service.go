package service

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/discordia-grupo01/metrics/internal/model"
	"github.com/discordia-grupo01/metrics/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrForbidden     = errors.New("forbidden")
	ErrInvalidID     = errors.New("invalid id")
	ErrInvalidPeriod = errors.New("invalid period")
)

type Authorizer interface {
	CanAdministerServer(context.Context, string, string) (bool, error)
	IsStaff(string) bool
}

type Service struct {
	repo       *repository.Repository
	authorizer Authorizer
}

func New(repo *repository.Repository, authorizer Authorizer) *Service {
	return &Service{repo: repo, authorizer: authorizer}
}

func (s *Service) ServerSummary(ctx context.Context, serverID, userID string, from, to time.Time) (model.ServerSummary, error) {
	if err := validateID(serverID); err != nil {
		return model.ServerSummary{}, err
	}
	if err := s.authorizeServer(ctx, serverID, userID); err != nil {
		return model.ServerSummary{}, err
	}
	return s.repo.Summary(ctx, serverID, from, to)
}

func (s *Service) ServerGrowth(ctx context.Context, serverID, userID string, from, to time.Time) ([]model.GrowthPoint, error) {
	if err := validateID(serverID); err != nil {
		return nil, err
	}
	if err := s.authorizeServer(ctx, serverID, userID); err != nil {
		return nil, err
	}
	return s.repo.Growth(ctx, serverID, from, to)
}

func (s *Service) authorizeServer(ctx context.Context, serverID, userID string) error {
	ok, err := s.authorizer.CanAdministerServer(ctx, serverID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

func validateID(value string) error {
	if _, err := uuid.Parse(value); err != nil {
		return fmt.Errorf("%w: expected UUID", ErrInvalidID)
	}
	return nil
}

func ParsePeriod(values url.Values, now time.Time) (time.Time, time.Time, error) {
	to := now.UTC()
	from := to.Add(-30 * 24 * time.Hour)
	var err error
	if value := values.Get("from"); value != "" {
		from, err = time.Parse(time.RFC3339, value)
	}
	if err == nil {
		if value := values.Get("to"); value != "" {
			to, err = time.Parse(time.RFC3339, value)
		}
	}
	if err != nil || !from.Before(to) || to.Sub(from) > 366*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: from/to must be RFC3339, from before to, maximum 366 days", ErrInvalidPeriod)
	}
	return from.UTC(), to.UTC(), nil
}

func WriteCSV(w http.ResponseWriter, summary model.ServerSummary) error {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="metrics.csv"`)
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"from", "to", "active_members", "messages_sent", "new_members"}); err != nil {
		return err
	}
	if err := writer.Write([]string{summary.From.Format(time.RFC3339), summary.To.Format(time.RFC3339),
		strconv.Itoa(summary.ActiveMembers), strconv.Itoa(summary.MessagesSent), strconv.Itoa(summary.NewMembers)}); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}

type ProjectionAuthorizer struct {
	repo  *repository.Repository
	staff map[string]struct{}
}

func NewProjectionAuthorizer(repo *repository.Repository, staffIDs []string) *ProjectionAuthorizer {
	staff := make(map[string]struct{}, len(staffIDs))
	for _, id := range staffIDs {
		if value := strings.TrimSpace(id); value != "" {
			staff[value] = struct{}{}
		}
	}
	return &ProjectionAuthorizer{repo: repo, staff: staff}
}

func (a *ProjectionAuthorizer) IsStaff(userID string) bool { _, ok := a.staff[userID]; return ok }

func (a *ProjectionAuthorizer) CanAdministerServer(ctx context.Context, serverID, userID string) (bool, error) {
	return a.repo.CanAdministerServer(ctx, serverID, userID)
}
