// Command semar-worker runs SEMAR's Temporal workers for one site and keeps
// the per-provider sync schedules in line with the registered providers.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
	sdklog "go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"

	"github.com/sulaimantok/semar/internal/config"
	"github.com/sulaimantok/semar/internal/driver/registry"
	"github.com/sulaimantok/semar/internal/metrics"
	"github.com/sulaimantok/semar/internal/secret"
	"github.com/sulaimantok/semar/internal/store"
	"github.com/sulaimantok/semar/internal/version"
	"github.com/sulaimantok/semar/internal/workflow"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "semar-worker:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadWorker(os.Getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	// The worker never migrates. During an upgrade semar-api (or
	// `semar-api migrate`) may still be applying migrations; syncing before
	// they land would query columns that do not exist yet.
	if err := st.WaitForSchema(ctx, 2*time.Second, func(applied, required int64) {
		log.Info("waiting for database migrations", "applied", applied, "required", required)
	}); err != nil {
		return err
	}

	codec, err := workflow.NewAESGCMCodec(cfg.PayloadKeyID, cfg.PayloadKeys)
	if err != nil {
		return err
	}
	c, err := client.Dial(client.Options{
		HostPort:      cfg.TemporalAddress,
		Namespace:     cfg.TemporalNamespace,
		Logger:        sdklog.NewStructuredLogger(log),
		DataConverter: workflow.NewDataConverter(codec),
	})
	if err != nil {
		return fmt.Errorf("connect to temporal at %s: %w", cfg.TemporalAddress, err)
	}
	defer c.Close()

	drivers := workflow.RegisteredDrivers{Providers: st}
	if cfg.KEKs != nil {
		keyring, err := secret.NewKeyring(cfg.KEKID, cfg.KEKs)
		if err != nil {
			return err
		}
		drivers.Resolver = &registry.Resolver{Credentials: st, Secrets: keyring}
	} else {
		log.Warn("SEMAR_KEK not set: only fake providers can be synced")
	}

	var syncMetrics *workflow.SyncMetrics
	if cfg.MetricsAddr != "" {
		info := version.Get()
		reg := metrics.NewRegistry(info.Version, info.Commit)
		syncMetrics = workflow.NewSyncMetrics(reg)
		healthz := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
		msrv, merrc := metrics.Serve(cfg.MetricsAddr, reg, map[string]http.Handler{"GET /healthz": healthz})
		defer func() { _ = msrv.Close() }()
		go func() {
			if err := <-merrc; err != nil {
				log.Error("metrics listener stopped", "error", err)
			}
		}()
		log.Info("serving metrics", "addr", cfg.MetricsAddr)
	}

	taskQueue := workflow.TaskQueueForSite(cfg.SiteID)
	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(workflow.SyncProviderWorkflow)
	w.RegisterActivity(&workflow.Activities{
		Drivers:     drivers,
		Inventory:   st,
		OrphanAfter: cfg.OrphanAfter,
		Metrics:     syncMetrics,
	})
	if err := w.Start(); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}
	defer w.Stop()

	info := version.Get()
	log.Info("starting semar-worker", "task_queue", taskQueue, "namespace", cfg.TemporalNamespace,
		"version", info.Version, "commit", info.Commit)

	// Rewrite existing schedules on start (re-encrypts them after a payload
	// key rotation), then only add missing and drop removed ones.
	reconcileSchedules(ctx, log, st, c, cfg, taskQueue, true)
	ticker := time.NewTicker(cfg.ReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down")
			return nil
		case <-ticker.C:
			reconcileSchedules(ctx, log, st, c, cfg, taskQueue, false)
		}
	}
}

// reconcileSchedules logs failures instead of exiting: the database or
// Temporal may be briefly unavailable (e.g. while semar-api migrates).
func reconcileSchedules(ctx context.Context, log *slog.Logger, st *store.Store, c client.Client, cfg config.Worker, taskQueue string, rewrite bool) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	providers, err := st.ListProviders(ctx)
	if err != nil {
		log.Error("list providers for schedule reconciliation", "error", err)
		return
	}
	var own, active []string
	for _, p := range providers {
		active = append(active, p.ID)
		site := p.SiteID
		if site == "" {
			site = "default"
		}
		if site == cfg.SiteID {
			own = append(own, p.ID)
		}
	}
	res, err := workflow.ReconcileSchedules(ctx, c, taskQueue, cfg.SyncInterval, own, active, rewrite)
	if err != nil {
		log.Error("reconcile sync schedules", "error", err)
	}
	if len(res.Ensured) > 0 || len(res.Removed) > 0 {
		log.Info("sync schedules reconciled", "ensured", res.Ensured, "removed", res.Removed)
	}
}
