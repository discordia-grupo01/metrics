package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/discordia-grupo01/metrics/internal/events"
	"github.com/discordia-grupo01/metrics/internal/handler"
	"github.com/discordia-grupo01/metrics/internal/health"
	"github.com/discordia-grupo01/metrics/internal/middleware"
	"github.com/discordia-grupo01/metrics/internal/repository"
	"github.com/discordia-grupo01/metrics/internal/service"
	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cfg := loadConfig()
	if cfg.environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	db, err := sql.Open("pgx", cfg.databaseURL())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	repo := repository.New(db)
	authorizer := service.NewProjectionAuthorizer(repo, strings.Split(cfg.staffUserIDs, ","))
	svc := service.New(repo, authorizer)
	healthMonitor := health.NewMonitor(
		health.ParseTargets(cfg.healthChecks),
		&http.Client{Timeout: cfg.healthTimeout},
		cfg.healthInterval,
		cfg.healthFailureThreshold,
		cfg.healthRecoveryThreshold,
	)
	h := handler.New(svc, repo, healthMonitor)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.rabbitURL != "" {
		go func() {
			if err := events.Consume(ctx, cfg.rabbitURL, cfg.exchange, cfg.queue, cfg.adminEventsHMACSecret, repo); err != nil && ctx.Err() == nil {
				log.Printf("event consumer stopped: %v", err)
			}
		}()
	}
	go healthMonitor.Start(ctx)

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.GET("/livez", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/readyz", func(c *gin.Context) {
		if err := db.PingContext(c); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	auth := router.Group("/v1", middleware.Auth(cfg.userIDHeader))
	auth.Use(func(c *gin.Context) { c.Set("authorizer", authorizer); c.Next() })
	auth.GET("/servers/:serverId/metrics", h.ServerSummary)
	auth.GET("/servers/:serverId/metrics/growth", h.ServerGrowth)
	auth.GET("/servers/:serverId/metrics/export.csv", h.Export)
	auth.GET("/metrics/global", h.Global)
	auth.GET("/status", h.Incidents)
	auth.GET("/health/services", h.ServiceHealth)
	auth.POST("/metrics/incidents", h.CreateIncident)
	auth.POST("/metrics/incidents/:id/resolve", h.ResolveIncident)

	server := &http.Server{Addr: cfg.host + ":" + cfg.port, Handler: router}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

type config struct {
	environment, host, port, userIDHeader                                                     string
	databaseHost, databasePort, databaseName, databaseUser, databasePassword, databaseSSLMode string
	databaseURLOverride                                                                       string
	rabbitURL, exchange, queue, staffUserIDs, adminEventsHMACSecret, healthChecks             string
	healthInterval, healthTimeout                                                             time.Duration
	healthFailureThreshold, healthRecoveryThreshold                                           int
}

func loadConfig() config {
	return config{
		environment: env("ENVIRONMENT", "development"), host: env("HOST", "0.0.0.0"), port: env("PORT", "8080"),
		userIDHeader: env("USER_ID_HEADER", ""), databaseHost: env("DATABASE_HOST", "localhost"),
		databasePort: env("DATABASE_PORT", "5432"), databaseName: env("DATABASE_NAME", "discordia_metrics"),
		databaseUser: env("DATABASE_USER", "discordia"), databasePassword: env("DATABASE_PASSWORD", "discordia"),
		databaseSSLMode: env("DATABASE_SSLMODE", "disable"), rabbitURL: os.Getenv("RABBITMQ_URL"),
		databaseURLOverride: os.Getenv("DATABASE_URL"),
		exchange:            env("EVENTS_EXCHANGE", "discordia.events"), queue: env("EVENTS_QUEUE", "metrics"),
		staffUserIDs: os.Getenv("STAFF_USER_IDS"), adminEventsHMACSecret: os.Getenv("ADMIN_EVENTS_HMAC_SECRET"),
		healthChecks:            os.Getenv("HEALTH_CHECKS"),
		healthInterval:          durationEnv("HEALTH_CHECK_INTERVAL", 30*time.Second),
		healthTimeout:           durationEnv("HEALTH_CHECK_TIMEOUT", 3*time.Second),
		healthFailureThreshold:  intEnv("HEALTH_FAILURE_THRESHOLD", 2),
		healthRecoveryThreshold: intEnv("HEALTH_RECOVERY_THRESHOLD", 2),
	}
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			return parsed
		}
		log.Printf("invalid %s=%q, using %s", key, value, fallback)
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		var parsed int
		if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil && parsed > 0 {
			return parsed
		}
		log.Printf("invalid %s=%q, using %d", key, value, fallback)
	}
	return fallback
}

func (c config) databaseURL() string {
	if c.databaseURLOverride != "" {
		return c.databaseURLOverride
	}
	return "postgres://" + url.QueryEscape(c.databaseUser) + ":" + url.QueryEscape(c.databasePassword) + "@" +
		c.databaseHost + ":" + c.databasePort + "/" + c.databaseName + "?sslmode=" + url.QueryEscape(c.databaseSSLMode)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
