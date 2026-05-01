package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	imsdb "github.com/zeotap/ims/db"
	"github.com/zeotap/ims/internal/api"
	"github.com/zeotap/ims/internal/auth"
	"github.com/zeotap/ims/internal/ingest"
	"github.com/zeotap/ims/internal/metrics"
	"github.com/zeotap/ims/internal/models"
	mongostore "github.com/zeotap/ims/internal/store/mongo"
	pgstore "github.com/zeotap/ims/internal/store/postgres"
	redisstore "github.com/zeotap/ims/internal/store/redis"
)

func main() {
	cfg := loadConfig()

	// --- Datastores ---
	pg, err := pgstore.New(cfg.DatabaseURL)
	must(err, "postgres")

	mg, err := mongostore.New(cfg.MongoURI)
	must(err, "mongo")

	rds, err := redisstore.New(cfg.RedisAddr, cfg.RedisPassword)
	must(err, "redis")

	// --- Run migrations ---
	runMigrations(pg)

	// --- Seed demo users with correct bcrypt hashes ---
	if err := pg.SeedUsers(context.Background(), "password123"); err != nil {
		log.Printf("[seed] warning: %v", err)
	} else {
		log.Println("[seed] demo users ready (password: password123)")
	}

	// --- Auth ---
	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)

	// --- Ingestion pipeline ---
	pipeline := ingest.New(cfg.IngressBuffer, cfg.WorkerCount, pg, mg, rds)
	ctx, cancel := context.WithCancel(context.Background())
	pipeline.Start(ctx)

	// --- Metrics ---
	done := make(chan struct{})
	go metrics.StartLogger(done)

	// --- Router ---
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://localhost:3001", "http://localhost:3002", "http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
	}))

	h := api.New(pg, mg, rds, issuer)

	// Public routes.
	r.Post("/auth/login", h.Login)
	r.Post("/auth/refresh", h.Refresh)
	r.Get("/health", h.Health)
	r.Get("/ready", h.Ready)

	// SSE — token accepted via cookie so browser clients work.
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(issuer))
		r.Get("/stream", api.SSEHandler(rds))
	})

	// Ingestion — requires producer role.
	r.Group(func(r chi.Router) {
		r.Use(ingest.RateLimitMiddleware(float64(cfg.RateLimitRPS), cfg.RateLimitBurst))
		r.Use(auth.Middleware(issuer))
		r.Use(auth.RequireRole(models.RoleProducer, models.RoleAdmin))
		r.Post("/api/v1/signals", ingest.Handler(pipeline))
	})

	// Dashboard API — requires responder or admin role.
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(issuer))
		r.Use(auth.RequireRole(models.RoleResponder, models.RoleAdmin))
		r.Get("/auth/me", h.Me)
		r.Get("/api/v1/incidents", h.ListIncidents)
		r.Get("/api/v1/incidents/{id}", h.GetIncident)
		r.Get("/api/v1/incidents/{id}/signals", h.GetIncidentSignals)
		r.Post("/api/v1/incidents/{id}/transition", h.TransitionIncident)
		r.Post("/api/v1/incidents/{id}/rca", h.SubmitRCA)
		r.Get("/api/v1/incidents/{id}/rca", h.GetRCA)
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("[ims] server starting on :%s (workers=%d buffer=%d)", cfg.Port, cfg.WorkerCount, cfg.IngressBuffer)

	// --- Graceful shutdown ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-quit
	log.Println("[ims] shutting down gracefully (30s)...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	cancel()
	close(done)
	pipeline.Stop()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}
	pg.Close()
	mg.Close(shutdownCtx)
	rds.Close()
	log.Println("[ims] shutdown complete")
}

func runMigrations(pg *pgstore.Store) {
	entries, err := imsdb.MigrationsFS.ReadDir("migrations")
	must(err, "read migrations dir")
	for _, e := range entries {
		data, err := imsdb.MigrationsFS.ReadFile("migrations/" + e.Name())
		must(err, "read migration "+e.Name())
		if err := pg.Exec(context.Background(), string(data)); err != nil {
			log.Printf("[migration] %s: %v (may already exist)", e.Name(), err)
		} else {
			log.Printf("[migration] applied %s", e.Name())
		}
	}
}

type config struct {
	DatabaseURL    string
	MongoURI       string
	RedisAddr      string
	RedisPassword  string
	JWTSecret      string
	JWTAccessTTL   time.Duration
	JWTRefreshTTL  time.Duration
	Port           string
	WorkerCount    int
	IngressBuffer  int
	RateLimitRPS   int
	RateLimitBurst int
}

func loadConfig() config {
	accessTTL, _ := time.ParseDuration(envOr("JWT_ACCESS_TTL", "15m"))
	refreshTTL, _ := time.ParseDuration(envOr("JWT_REFRESH_TTL", "168h"))
	return config{
		DatabaseURL:    envOr("DATABASE_URL", "postgres://ims:ims_secret@localhost:5432/ims?sslmode=disable"),
		MongoURI:       envOr("MONGO_URI", "mongodb://ims:ims_secret@localhost:27017/ims?authSource=admin"),
		RedisAddr:      envOr("REDIS_ADDR", "localhost:6379"),
		RedisPassword:  envOr("REDIS_PASSWORD", "ims_secret"),
		JWTSecret:      envOr("JWT_SECRET", "dev_secret_change_me"),
		JWTAccessTTL:   accessTTL,
		JWTRefreshTTL:  refreshTTL,
		Port:           envOr("PORT", "8080"),
		WorkerCount:    envInt("WORKER_COUNT", 16),
		IngressBuffer:  envInt("INGRESS_BUFFER", 50000),
		RateLimitRPS:   envInt("RATE_LIMIT_RPS", 20000),
		RateLimitBurst: envInt("RATE_LIMIT_BURST", 5000),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func must(err error, label string) {
	if err != nil {
		log.Fatalf("fatal: %s: %v", label, err)
	}
}
