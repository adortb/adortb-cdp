package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"github.com/adortb/adortb-cdp/internal/api"
	"github.com/adortb/adortb-cdp/internal/audience"
	"github.com/adortb/adortb-cdp/internal/export"
	"github.com/adortb/adortb-cdp/internal/ingestion"
	"github.com/adortb/adortb-cdp/internal/journey"
	"github.com/adortb/adortb-cdp/internal/metrics"
	"github.com/adortb/adortb-cdp/internal/profile"
)

const (
	listenAddr = ":8102"
	pgDSN      = "postgres://postgres:postgres@localhost:5432/adortb_cdp?sslmode=disable"
	redisAddr  = "localhost:6379"
	kafkaBroker = "localhost:9092"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// ---- 存储层 ----
	db := mustOpenPG(envOr("POSTGRES_DSN", pgDSN), logger)
	defer db.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr:     envOr("REDIS_ADDR", redisAddr),
		PoolSize: 50,
	})
	defer rdb.Close()

	// ---- Profile 层 ----
	profileStore := profile.NewPGStore(db, rdb)
	merger := profile.NewMerger(profileStore, db)
	enricher := profile.NewEnricher(profileStore, logger)
	_ = enricher

	// ---- Audience 层 ----
	audienceStore := audience.NewPGStore(db)
	counter := ingestion.NewPGEventCounter(db)
	evaluator := audience.NewEvaluator(counter)
	audienceBuilder := audience.NewBuilder(audienceStore, evaluator, profileStore)

	// ---- Journey 层 ----
	journeyStore := journey.NewPGStore(db)
	actionExecutor := journey.NewDefaultExecutor(profileStore, logger)
	orchestrator := journey.NewOrchestrator(journeyStore, actionExecutor, logger)
	triggerEval := journey.NewTriggerEvaluator(journeyStore, orchestrator, profileStore, logger)

	// ---- Ingestion ----
	mapper := ingestion.NewMapper()
	brokers := []string{envOr("KAFKA_BROKER", kafkaBroker)}
	consumer := ingestion.NewConsumer(brokers, nil, mapper, profileStore, audienceBuilder, triggerEval, logger)

	// ---- Export ----
	exporter := export.NewExporter(audienceStore, logger)

	// ---- Metrics ----
	m := metrics.New()

	// ---- HTTP API ----
	handler := api.NewHandler(
		profileStore, merger,
		audienceStore, audienceBuilder,
		journeyStore, orchestrator,
		exporter, logger,
	)
	server := api.NewServer(handler, m)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	consumer.Start(ctx)

	httpServer := &http.Server{
		Addr:         envOr("LISTEN_ADDR", listenAddr),
		Handler:      server,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("cdp server starting", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
			cancel()
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
		logger.Info("shutdown signal received")
	case <-ctx.Done():
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	consumer.Close()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown error", "error", err)
	}
	logger.Info("cdp server stopped")
}

func mustOpenPG(dsn string, logger *slog.Logger) *sql.DB {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		logger.Error("open postgres failed", "error", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		logger.Warn("postgres ping failed (will retry on first request)", "error", err)
	}
	return db
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
