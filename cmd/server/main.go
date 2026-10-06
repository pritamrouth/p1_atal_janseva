package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ataljanseva/janseva-chatbot/config"
	"github.com/ataljanseva/janseva-chatbot/internal/bot"
	"github.com/ataljanseva/janseva-chatbot/internal/db"
	"github.com/ataljanseva/janseva-chatbot/internal/inactivity"
	"github.com/ataljanseva/janseva-chatbot/internal/store"
	"github.com/ataljanseva/janseva-chatbot/internal/whatsapp"
	"github.com/ataljanseva/janseva-chatbot/internal/worker"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

func main() {
	// ── Structured logging (JSON in prod, text in dev) ────────────────────────
	if os.Getenv("ENV") == "production" {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})))
	}

	// ── Config ────────────────────────────────────────────────────────────────
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("godotenv", "err", err)
	}
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	// ── Context for clean shutdown ────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── Redis ─────────────────────────────────────────────────────────────────
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr,
		Password:     cfg.RedisPassword,
		DB:           cfg.RedisDB,
		PoolSize:     cfg.WorkerCount + 10,
		MinIdleConns: 5,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})

	sessionStore, err := store.New(rdb)
	if err != nil {
		slog.Error("redis", "err", err)
		os.Exit(1)
	}
	slog.Info("redis connected", "addr", cfg.RedisAddr)

	// ── PostgreSQL ────────────────────────────────────────────────────────────
	repo, err := db.New(cfg.DatabaseURL, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns)
	if err != nil {
		slog.Error("database", "err", err)
		os.Exit(1)
	}
	defer repo.Close()
	slog.Info("database connected")

	// ── Migrations (complaints / meetings tables) ─────────────────────────────
	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}
	if err := repo.RunMigrations(ctx, migrationsDir); err != nil {
		slog.Error("migrations", "err", err)
		os.Exit(1)
	}

	// ── WhatsApp client (Meta Cloud API) ──────────────────────────────────────
	messenger := whatsapp.NewMetaClient(cfg.WAPhoneNumberID, cfg.WAAccessToken)
	slog.Info("using Meta WhatsApp provider")

	// ── Inactivity monitor ────────────────────────────────────────────────────
	// When a user is silent for 90 s:
	//   1. Their session is deleted from Redis (next message starts fresh).
	//   2. upsert_citizen_ivr is called to persist the citizen record in PostgreSQL.
	//      Only fires when the user completed at least pincode + leader selection.
	if cfg.EnableInactivityMonitor {
		inactiveMonitor := inactivity.New(rdb, func(cbCtx context.Context, phone string, snap inactivity.SessionSnapshot) {
			slog.Debug("user session expired (inactivity)", "phone", phone, "lang", snap.Lang)

			// Skip the DB call if the user never completed the flow
			// (no pincode or leader means the conversation was incomplete).
			if snap.Pincode == "" || snap.NagarsevakID == "" {
				slog.Debug("inactivity: incomplete session, skipping IVR upsert",
					"phone", phone,
					"pincode", snap.Pincode,
					"nagarsevak_id", snap.NagarsevakID,
				)
				return
			}

			lang := strings.ToLower(snap.Lang)
			if lang == "" {
				lang = "en"
			}

			if err := repo.UpsertCitizenIVR(
				cbCtx,
				phone,
				lang,
				snap.Pincode,
				snap.NagarsevakWard,
				snap.NagarsevakID,
			); err != nil {
				slog.Error("inactivity: UpsertCitizenIVR failed",
					"phone", phone,
					"lang", lang,
					"pincode", snap.Pincode,
					"ward", snap.NagarsevakWard,
					"nagarsevak_id", snap.NagarsevakID,
					"err", err,
				)
				return
			}

			slog.Info("inactivity: citizen IVR upserted",
				"phone", phone,
				"lang", lang,
				"pincode", snap.Pincode,
				"ward", snap.NagarsevakWard,
				"nagarsevak_id", snap.NagarsevakID,
			)
		})
		inactiveMonitor.Start(ctx)
		slog.Info("inactivity monitor enabled",
			"timeout", inactivity.InactivityTimeout,
			"action", "session_cleanup_and_ivr_upsert",
		)
		// Bot + Worker pool
		runServer(ctx, cfg, messenger, sessionStore, repo, inactiveMonitor)
		return
	}

	slog.Info("inactivity monitor disabled")
	skipMonitor := inactivity.New(rdb, func(context.Context, string, inactivity.SessionSnapshot) {})
	runServer(ctx, cfg, messenger, sessionStore, repo, skipMonitor)
}

// runServer wires the bot, worker pool and HTTP server.
func runServer(
	ctx context.Context,
	cfg *config.Config,
	messenger whatsapp.Messenger,
	sessionStore *store.Store,
	repo *db.Repo,
	inactiveMonitor *inactivity.Monitor,
) {
	botHandler := bot.New(messenger, sessionStore, repo, inactiveMonitor)

	pool := worker.New(cfg.WorkerCount, cfg.QueueDepth, botHandler.HandleRaw)
	pool.Start()

	mux := http.NewServeMux()

	webhook := &webhookHandler{
		verifyToken: cfg.WAVerifyToken,
		pool:        pool,
	}
	mux.Handle("/webhook", webhook)

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","queue_len":%d,"queue_cap":%d,"dropped_messages":%d}`,
			pool.QueueLen(), cfg.QueueDepth, pool.DroppedCount())
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("Ataljanseva WhatsApp Bot started",
		"port", cfg.Port,
		"provider", "meta",
		"workers", cfg.WorkerCount,
		"queue_depth", cfg.QueueDepth,
		"inactivity_timeout", inactivity.InactivityTimeout,
	)

	// Run server in a goroutine so we can listen for shutdown signals
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	// Block until signal
	<-ctx.Done()
	slog.Info("shutdown signal received, draining…")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	slog.Info("server stopped cleanly")
}
