package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/discordia-grupo01/metrics/internal/model"
	"github.com/discordia-grupo01/metrics/internal/repository"
)

var (
	ErrForbidden             = errors.New("forbidden")
	ErrInvalidPeriod         = errors.New("invalid period")
	ErrDependencyUnavailable = errors.New("authorization dependency unavailable")
)

type Authorizer interface {
	CanAdministerServer(context.Context, string, string, string) (bool, error)
	IsStaff(string) bool
}

type Service struct {
	repo       *repository.Repository
	authorizer Authorizer
}

func New(repo *repository.Repository, authorizer Authorizer) *Service {
	return &Service{repo: repo, authorizer: authorizer}
}

func (s *Service) ServerSummary(ctx context.Context, serverID, userID, authHeader string, from, to time.Time) (model.ServerSummary, error) {
	if err := s.authorizeServer(ctx, serverID, userID, authHeader); err != nil {
		return model.ServerSummary{}, err
	}
	return s.repo.Summary(ctx, serverID, from, to)
}

func (s *Service) ServerGrowth(ctx context.Context, serverID, userID, authHeader string, from, to time.Time) ([]model.GrowthPoint, error) {
	if err := s.authorizeServer(ctx, serverID, userID, authHeader); err != nil {
		return nil, err
	}
	return s.repo.Growth(ctx, serverID, from, to)
}

func (s *Service) authorizeServer(ctx context.Context, serverID, userID, authHeader string) error {
	ok, err := s.authorizer.CanAdministerServer(ctx, serverID, userID, authHeader)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
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

type HTTPAuthorizer struct {
	serversURL string
	client     *http.Client
	staff      map[string]struct{}
}

func NewHTTPAuthorizer(serversURL string, staffIDs []string) *HTTPAuthorizer {
	staff := make(map[string]struct{}, len(staffIDs))
	for _, id := range staffIDs {
		if value := strings.TrimSpace(id); value != "" {
			staff[value] = struct{}{}
		}
	}
	return &HTTPAuthorizer{serversURL: strings.TrimRight(serversURL, "/"), client: &http.Client{Timeout: 5 * time.Second}, staff: staff}
}

func (a *HTTPAuthorizer) IsStaff(userID string) bool { _, ok := a.staff[userID]; return ok }

func (a *HTTPAuthorizer) CanAdministerServer(ctx context.Context, serverID, userID, authHeader string) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/v1/servers/%s/members/%s/permissions", a.serversURL, url.PathEscape(serverID), url.PathEscape(userID)), nil)
	if err != nil {
		return false, err
	}
	if authHeader != "" {
		request.Header.Set("Authorization", authHeader)
	}
	response, err := a.client.Do(request)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrDependencyUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, fmt.Errorf("%w: servers authorization returned %s", ErrDependencyUnavailable, response.Status)
	}
	var payload struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return false, fmt.Errorf("%w: invalid authorization response: %v", ErrDependencyUnavailable, err)
	}
	for _, permission := range payload.Permissions {
		if permission == "ADMINISTRATOR" {
			return true, nil
		}
	}
	return false, nil
}
