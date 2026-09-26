package main

import (
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	_ "image/jpeg"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aside/core/internal/observe"
)

type API struct {
	db         *pgxpool.Pool
	env        string
	sessionKey []byte
	// mailer replaces real email delivery when set (integration tests only).
	mailer func(to, subject, body string) bool
	// mailCapture sees every outgoing message in full (integration tests only).
	mailCapture func(emailMessage)
	// calTokens caches Google access tokens per seller.
	calTokens accessTokens
	// redis shares rate-limit counts across API instances (nil: local only).
	redis *redisClient
	// rateLimitScale multiplies every rate limit (integration tests only).
	rateLimitScale int
	// media stores profile photos in object storage (nil: in PostgreSQL).
	media *objectStore
	// retentionRan limits the retention sweep to once an hour per instance.
	retentionMu  sync.Mutex
	retentionRan time.Time
	logger       *slog.Logger
	vapid        *vapidKeys // web push; nil when not configured
	reporter     *observe.Reporter
	metrics      *observe.Metrics
}

// log returns the configured structured logger (the default logger in tests).
func (a *API) log() *slog.Logger {
	if a.logger != nil {
		return a.logger
	}
	return slog.Default()
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) > 1 && os.Args[1] == "vapid-keys" {
		public, private, err := generateVAPIDKeys()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", public, private)
		return
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required; JSON fallback is disabled")
	}
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("connect PostgreSQL: %v", err)
	}
	defer db.Close()
	if err = db.Ping(ctx); err != nil {
		log.Fatalf("ping PostgreSQL: %v", err)
	}
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err = runMigrations(ctx, db); err != nil {
			log.Fatalf("migrations failed: %v", err)
		}
		log.Print("database migrations applied")
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "bootstrap-admin" {
		if envOr("APP_ENV", "local") == "production" && os.Getenv("OPS_MFA_ENCRYPTION_KEY") == "" {
			log.Fatal("OPS_MFA_ENCRYPTION_KEY is required")
		}
		if len(os.Args) != 3 {
			log.Fatal("usage: aside-api bootstrap-admin verified-email (set OPS_BOOTSTRAP_TOTP_SECRET securely)")
		}
		encodedSecret := os.Getenv("OPS_BOOTSTRAP_TOTP_SECRET")
		_ = os.Unsetenv("OPS_BOOTSTRAP_TOTP_SECRET")
		if encodedSecret == "" {
			log.Fatal("OPS_BOOTSTRAP_TOTP_SECRET is required")
		}
		if err := bootstrapAdmin(ctx, db, strings.ToLower(strings.TrimSpace(os.Args[2])), encodedSecret); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Operations owner and MFA secret configured.")
		return
	}
	env := envOr("APP_ENV", "local")
	if env == "production" {
		problems, warnings := productionConfigProblems(envLookup)
		for _, w := range warnings {
			slog.Warn("configuration", "warning", w)
		}
		if len(problems) > 0 {
			for _, p := range problems {
				slog.Error("configuration", "problem", p)
			}
			log.Fatalf("refusing to start: %d configuration problem(s); see the log lines above and docs/DEPLOYMENT.md", len(problems))
		}
	}
	sessionSecret := os.Getenv("SESSION_SECRET")
	if len(sessionSecret) < 32 {
		if env == "production" {
			log.Fatal("SESSION_SECRET must contain at least 32 bytes in production")
		}
		if sessionSecret == "" {
			sessionSecret = "local-only-session-secret-change-before-use"
		}
	}
	reporter, reporterErr := observe.NewReporter(os.Getenv("SENTRY_DSN"), env, os.Getenv("APP_RELEASE"))
	logger := observe.NewLogger(os.Stdout, reporter)
	slog.SetDefault(logger) // the standard log package now writes JSON too
	if reporterErr != nil {
		logger.Warn("error reporting disabled", "reason", reporterErr.Error())
	}
	defer reporter.Close(5 * time.Second)
	a := &API{db: db, env: env, sessionKey: []byte(sessionSecret), logger: logger, reporter: reporter, metrics: observe.NewMetrics()}
	// End-to-end tests sign many people up from one address; a local API may
	// raise its rate limits for them. Ignored everywhere else.
	if scale, scaleErr := strconv.Atoi(os.Getenv("LOCAL_RATE_LIMIT_SCALE")); env == "local" && scaleErr == nil && scale > 1 && scale <= 100 {
		a.rateLimitScale = scale
	}
	if a.media, err = newObjectStoreFromEnv(env); err != nil {
		log.Fatal(err)
	}
	if raw := os.Getenv("REDIS_URL"); raw != "" {
		rc, err := newRedisClient(raw)
		if err != nil {
			log.Fatal(err)
		}
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		if err = rc.ping(pingCtx); err != nil {
			logger.Error("Redis did not answer; rate limits are local to this instance until it does", "error", err.Error())
		}
		cancel()
		a.redis = rc
	}
	a.metrics.AddGauges(a.operationalGauges)
	go a.providerEventWorker(ctx)
	go a.runWatchdog(ctx)
	go a.runLifecycleWorker(ctx)
	addr := envOr("API_ADDR", "127.0.0.1:8081")
	if a.emailConfigured() {
		go a.runNotificationWorker(ctx)
		go a.runBroadcastWorker(ctx)
	}
	if a.googleCalendarConfigured() {
		go a.runCalendarWorker(ctx)
	}
	if vapid, vapidErr := loadVAPID(); vapidErr != nil {
		logger.Error("web push is off: bad VAPID keys", "error", vapidErr.Error())
	} else if vapid != nil {
		a.vapid = vapid
		go a.runPushWorker(ctx)
	}
	server := &http.Server{Addr: addr, Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	logger.Info("API listening", "addr", addr, "env", env, "provider_checkout_enabled", a.providerCheckoutConfigured(), "error_reporting", reporter != nil, "metrics_enabled", os.Getenv("METRICS_TOKEN") != "", "shared_rate_limits", a.redis != nil, "object_storage", a.media != nil)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	select {
	case err = <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("API server stopped", "error", err.Error())
			reporter.Close(5 * time.Second)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err = server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful API shutdown failed", "error", err.Error())
			_ = server.Close()
		}
	}
}

func decodeBase32Secret(s string) ([]byte, error) {
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(strings.ToUpper(s), "="))
}

func bootstrapAdmin(ctx context.Context, db *pgxpool.Pool, email, encodedSecret string) error {
	if !emailPattern.MatchString(email) {
		return fmt.Errorf("a verified email address is required")
	}
	secret, err := decodeBase32Secret(encodedSecret)
	if err != nil || len(secret) < 20 {
		return fmt.Errorf("MFA secret must be a base32 secret of at least 160 bits")
	}
	encrypted, err := encryptMFASecret(secret)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT u.id::text FROM users u JOIN user_identities i ON i.user_id=u.id WHERE i.type='email' AND i.normalized_identifier=$1 AND i.verified_at IS NOT NULL AND u.status='active'`, email).Scan(&userID); err != nil {
		return fmt.Errorf("email must belong to an existing verified active account")
	}
	for _, permission := range []string{"ops:read", "ops:account:restrict", "ops:session:revoke", "ops:booking:resolve", "ops:settlement:import", "ops:seller:approve", "ops:refund:approve", "ops:marketing:send"} {
		if _, err = tx.Exec(ctx, `INSERT INTO admin_grants(id,user_id,permission,granted_by) VALUES(gen_random_uuid(),$1,$2,$1) ON CONFLICT DO NOTHING`, userID, permission); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admin_mfa(user_id,encrypted_secret) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET encrypted_secret=EXCLUDED.encrypted_secret,updated_at=now()`, userID, encrypted); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_id,action,target_id,safe_summary) VALUES(gen_random_uuid(),$1,'admin.bootstrap',$1,jsonb_build_object('permissions',ARRAY['ops:read','ops:account:restrict','ops:session:revoke','ops:booking:resolve','ops:settlement:import','ops:seller:approve','ops:refund:approve','ops:marketing:send']))`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func runMigrations(ctx context.Context, db *pgxpool.Pool) error {
	dir := envOr("MIGRATION_DIR", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if _, err = db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(918273645)`); err != nil {
		return err
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(918273645)`) }()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version := strings.TrimSuffix(entry.Name(), ".sql")
		var applied bool
		if err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		b, readErr := os.ReadFile(dir + string(os.PathSeparator) + entry.Name())
		if readErr != nil {
			return readErr
		}
		script := "BEGIN;" + string(b) + "; INSERT INTO schema_migrations(version) VALUES ('" + strings.ReplaceAll(version, "'", "''") + "'); COMMIT;"
		if _, err = conn.Conn().PgConn().Exec(ctx, script).ReadAll(); err != nil {
			_, _ = conn.Exec(ctx, "ROLLBACK")
			return fmt.Errorf("migration %s failed: %w", version, err)
		}
	}
	return nil
}

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
