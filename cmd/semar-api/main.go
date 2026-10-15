// Command semar-api serves the SEMAR HTTP API.
//
//	semar-api                       serve the API
//	semar-api migrate               apply database migrations and exit
//	semar-api reset-password <user> set a local password read from stdin (break-glass)
//	semar-api audit-verify          verify the audit log hash chain and exit
//	semar-api kek-rewrap            rewrap every credential with the current KEK and exit
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sulaimantok/semar/internal/api"
	"github.com/sulaimantok/semar/internal/auth"
	"github.com/sulaimantok/semar/internal/config"
	"github.com/sulaimantok/semar/internal/metrics"
	"github.com/sulaimantok/semar/internal/secret"
	"github.com/sulaimantok/semar/internal/store"
	"github.com/sulaimantok/semar/internal/version"
)

func main() {
	var err error
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "":
		err = run()
	case "audit-verify":
		err = auditVerify()
	case "kek-rewrap":
		err = kekRewrap()
	case "migrate":
		err = migrate()
	case "reset-password":
		err = resetPassword(os.Args[2:], os.Stdin)
	default:
		err = fmt.Errorf("unknown command %q (want migrate, reset-password, audit-verify or kek-rewrap)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "semar-api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadAPI(os.Getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	startCtx, cancelStart := context.WithTimeout(context.Background(), time.Minute)
	defer cancelStart()
	if cfg.MigrateOnStart {
		if err := store.Migrate(startCtx, cfg.DatabaseURL); err != nil {
			return err
		}
		log.Info("database migrations applied")
	}
	st, err := store.Open(startCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	if cfg.BootstrapAdminPassword != "" {
		if err := bootstrapAdmin(startCtx, st, cfg.BootstrapAdminPassword, log); err != nil {
			return err
		}
	}
	keyring, err := secret.NewKeyring(cfg.KEKID, cfg.KEKs)
	if err != nil {
		return err
	}
	if !cfg.CookieSecure {
		log.Warn("session cookie is not Secure; use only on localhost over plain HTTP")
	}

	var reg *metrics.Registry
	if cfg.MetricsAddr != "" {
		info := version.Get()
		reg = metrics.NewRegistry(info.Version, info.Commit)
		msrv, merrc := metrics.Serve(cfg.MetricsAddr, reg, nil)
		defer func() { _ = msrv.Close() }()
		go func() {
			if err := <-merrc; err != nil {
				log.Error("metrics listener stopped", "error", err)
			}
		}()
		log.Info("serving metrics", "addr", cfg.MetricsAddr)
	}

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.NewHandler(api.Options{
			Logger:         log,
			Checks:         map[string]api.ReadinessCheck{"database": st.Ping},
			Settings:       st,
			Auth:           st,
			Audit:          st,
			SessionTTL:     cfg.SessionTTL,
			CookieSecure:   cfg.CookieSecure,
			Credentials:    st,
			Secrets:        keyring,
			Inventory:      st,
			TrustedOrigins: cfg.TrustedOrigins,
			Metrics:        reg,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		info := version.Get()
		log.Info("starting semar-api", "addr", cfg.HTTPAddr, "version", info.Version, "commit", info.Commit)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down", "timeout", cfg.ShutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return <-errc
}

// bootstrapAdmin creates the first admin when the database has no users.
func bootstrapAdmin(ctx context.Context, st *store.Store, password string, log *slog.Logger) error {
	hash, err := auth.HashPassword(password, auth.DefaultArgon2)
	if err != nil {
		return fmt.Errorf("SEMAR_BOOTSTRAP_ADMIN_PASSWORD: %w", err)
	}
	created, err := st.EnsureBootstrapAdmin(ctx, "admin", hash)
	if err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	if !created {
		log.Info("users exist; SEMAR_BOOTSTRAP_ADMIN_PASSWORD ignored and can be removed")
		return nil
	}
	if _, err := st.AppendAudit(ctx, store.AuditEvent{
		Actor: "system", Action: "user.bootstrap_admin", Target: "admin", Outcome: store.AuditSuccess,
	}); err != nil {
		return fmt.Errorf("audit bootstrap admin: %w", err)
	}
	log.Warn("bootstrap admin user created; remove SEMAR_BOOTSTRAP_ADMIN_PASSWORD from the environment")
	return nil
}

// auditVerify checks the audit hash chain; it exits non-zero if broken.
// migrate applies pending migrations and exits. It needs only
// SEMAR_DATABASE_URL, so it can run as the database owner role while the
// API itself runs as a role that owns nothing (docs/runbooks/database-roles.md).
func migrate() error {
	dbURL := os.Getenv("SEMAR_DATABASE_URL")
	if dbURL == "" {
		return errors.New("SEMAR_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := store.Migrate(ctx, dbURL); err != nil {
		return err
	}
	fmt.Println("migrations applied")
	return nil
}

// resetPassword is the break-glass path for a lost local password: it reads
// the new password from stdin (never from arguments or the environment,
// which end up in shell history and process listings), re-enables the
// account, ends its sessions and records the action in the audit log.
func resetPassword(args []string, stdin io.Reader) error {
	if len(args) != 1 || args[0] == "" {
		return errors.New("usage: semar-api reset-password <username>  (new password on stdin)")
	}
	username := strings.ToLower(strings.TrimSpace(args[0]))
	dbURL := os.Getenv("SEMAR_DATABASE_URL")
	if dbURL == "" {
		return errors.New("SEMAR_DATABASE_URL is required")
	}
	line, err := bufio.NewReader(io.LimitReader(stdin, 4096)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read password: %w", err)
	}
	password := strings.TrimRight(line, "\r\n")
	hash, err := auth.HashPassword(password, auth.DefaultArgon2)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	ended, err := st.ResetPassword(ctx, username, hash)
	outcome := store.AuditSuccess
	if err != nil {
		outcome = store.AuditFailure
	}
	if _, aerr := st.AppendAudit(ctx, store.AuditEvent{
		Actor: "break-glass-cli", Action: "user.reset_password", Target: username, Outcome: outcome,
	}); aerr != nil {
		return fmt.Errorf("audit log unavailable: %w", aerr)
	}
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no user %q", username)
	}
	if err != nil {
		return err
	}
	fmt.Printf("password reset for %s; account enabled; %d session(s) ended\n", username, ended)
	return nil
}

func auditVerify() error {
	cfg, err := config.LoadAPI(os.Getenv)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := st.VerifyAuditChain(ctx)
	if err != nil {
		return fmt.Errorf("audit chain verification failed after %d valid events: %w", n, err)
	}
	fmt.Printf("audit chain OK: %d events verified\n", n)
	return nil
}

// kekRewrap moves every credential to the current KEK so previous KEKs can be
// removed from SEMAR_KEK_PREVIOUS afterwards (ADR-0004).
func kekRewrap() error {
	cfg, err := config.LoadAPI(os.Getenv)
	if err != nil {
		return err
	}
	keyring, err := secret.NewKeyring(cfg.KEKID, cfg.KEKs)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := st.RewrapCredentials(ctx, keyring)
	if err != nil {
		return fmt.Errorf("rewrap stopped after %d credentials: %w", n, err)
	}
	if _, err := st.AppendAudit(ctx, store.AuditEvent{
		Actor: "system", Action: "credential.kek_rewrap", Target: cfg.KEKID, Outcome: store.AuditSuccess,
		Details: map[string]string{"rewrapped": fmt.Sprint(n)},
	}); err != nil {
		return fmt.Errorf("audit rewrap: %w", err)
	}
	fmt.Printf("rewrapped %d credentials to KEK %q\n", n, cfg.KEKID)
	return nil
}
