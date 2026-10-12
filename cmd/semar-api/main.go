// Command semar-api serves the SEMAR HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sulaimantok/semar/internal/api"
	"github.com/sulaimantok/semar/internal/config"
	"github.com/sulaimantok/semar/internal/store"
	"github.com/sulaimantok/semar/internal/version"
)

func main() {
	if err := run(); err != nil {
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

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.NewHandler(api.Options{
			Logger: log,
			Checks: map[string]api.ReadinessCheck{"database": st.Ping},
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
