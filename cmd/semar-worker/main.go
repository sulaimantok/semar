// Command semar-worker runs SEMAR's Temporal workers and keeps the
// per-provider sync schedules in place.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.temporal.io/sdk/client"
	sdklog "go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"

	"github.com/sulaimantok/semar/internal/config"
	"github.com/sulaimantok/semar/internal/driver/fake"
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

	// Until providers are loaded from the database, sync Fake Driver providers.
	drivers := workflow.StaticDrivers{}
	var seed uint64
	for _, id := range cfg.FakeProviders {
		seed++
		drivers[id] = fake.New(fake.Config{Seed: seed})
	}

	taskQueue := workflow.TaskQueueForSite(cfg.SiteID)
	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(workflow.SyncProviderWorkflow)
	w.RegisterActivity(&workflow.Activities{Drivers: drivers})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for id := range drivers {
		if err := workflow.EnsureSyncSchedule(ctx, c, id, taskQueue, cfg.SyncInterval); err != nil {
			return err
		}
		log.Info("sync schedule ensured", "provider", id, "every", cfg.SyncInterval.String())
	}

	info := version.Get()
	log.Info("starting semar-worker", "task_queue", taskQueue, "namespace", cfg.TemporalNamespace,
		"version", info.Version, "commit", info.Commit)
	return w.Run(worker.InterruptCh())
}
