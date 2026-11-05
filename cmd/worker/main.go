package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Wh1teJ0ker/birkenwald-recruit/internal/platform"
	"github.com/Wh1teJ0ker/birkenwald-recruit/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := platform.LoadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := platform.OpenDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = platform.Migrate(ctx, pool); err != nil {
		return err
	}
	slog.Info("worker started", "version", platform.Version)
	err = (&worker.Worker{DB: pool, Config: cfg}).Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
