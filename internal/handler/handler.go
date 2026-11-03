package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/discordia-grupo01/metrics/internal/middleware"
	"github.com/discordia-grupo01/metrics/internal/model"
	"github.com/discordia-grupo01/metrics/internal/repository"
	"github.com/discordia-grupo01/metrics/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	service *service.Service
	repo    *repository.Repository
}

func New(svc *service.Service, repo *repository.Repository) *Handler {
	return &Handler{service: svc, repo: repo}
}

func (h *Handler) ServerSummary(c *gin.Context) {
	from, to, ok := h.period(c)
	if !ok {
		return
	}
	summary, err := h.service.ServerSummary(c, c.Param("serverId"), middleware.UserID(c), from, to)
	if h.respondError(c, err) {
		return
	}
	c.JSON(http.StatusOK, summary)
}

func (h *Handler) ServerGrowth(c *gin.Context) {
	from, to, ok := h.period(c)
	if !ok {
		return
	}
	points, err := h.service.ServerGrowth(c, c.Param("serverId"), middleware.UserID(c), from, to)
	if h.respondError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"from": from, "to": to, "has_activity": len(points) > 0, "points": points})
}

func (h *Handler) Export(c *gin.Context) {
	from, to, ok := h.period(c)
	if !ok {
		return
	}
	summary, err := h.service.ServerSummary(c, c.Param("serverId"), middleware.UserID(c), from, to)
	if h.respondError(c, err) {
		return
	}
	if err := service.WriteCSV(c.Writer, summary); err != nil {
		c.Error(err)
	}
}

func (h *Handler) Global(c *gin.Context) {
	authorizer := c.MustGet("authorizer").(service.Authorizer)
	if !authorizer.IsStaff(middleware.UserID(c)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "staff role required"})
		return
	}
	from, to, ok := h.period(c)
	if !ok {
		return
	}
	summary, err := h.repo.Global(c, from, to)
	if h.respondError(c, err) {
		return
	}
	c.JSON(http.StatusOK, summary)
}

func (h *Handler) Incidents(c *gin.Context) {
	incidents, err := h.repo.Incidents(c, true)
	if h.respondError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"incidents": incidents})
}

func (h *Handler) CreateIncident(c *gin.Context) {
	authorizer := c.MustGet("authorizer").(service.Authorizer)
	if !authorizer.IsStaff(middleware.UserID(c)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "staff role required"})
		return
	}
	var input model.IncidentInput
	if c.ShouldBindJSON(&input) != nil ||
		strings.TrimSpace(input.Title) == "" ||
		strings.TrimSpace(input.Description) == "" ||
		len([]rune(strings.TrimSpace(input.Title))) > 160 ||
		len([]rune(strings.TrimSpace(input.Description))) > 5000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title and description are required and within their length limits"})
		return
	}
	incident, err := h.repo.CreateIncident(c, input)
	if h.respondError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, incident)
}

func (h *Handler) ResolveIncident(c *gin.Context) {
	authorizer := c.MustGet("authorizer").(service.Authorizer)
	if !authorizer.IsStaff(middleware.UserID(c)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "staff role required"})
		return
	}
	if _, err := uuid.Parse(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid incident id"})
		return
	}
	if h.respondError(c, h.repo.ResolveIncident(c, c.Param("id"))) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) period(c *gin.Context) (from, to time.Time, ok bool) {
	from, to, err := service.ParsePeriod(c.Request.URL.Query(), time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return from, to, false
	}
	return from, to, true
}

func (h *Handler) respondError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	switch {
	case errors.Is(err, service.ErrInvalidID):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrInvalidPeriod):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "administrator permission required"})
	case errors.Is(err, repository.ErrIncidentNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
	return true
}
