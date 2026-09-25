// Command sqs-to-smpp-gateway receives the queue messages pushed over HTTP
// (e.g. by a Scaleway Queues trigger) and sends them as SMS to an SMSC over
// SMPP.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/config"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/handler"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/smpp"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	client := smpp.NewClient(cfg.SMPP, logger)
	srv := &http.Server{
		Addr:              net.JoinHostPort("", cfg.Port),
		Handler:           handler.New(client, logger),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", srv.Addr)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	// Let in-flight sends finish (an interrupted request is retried by the
	// trigger, which may send the SMS twice), then unbind.
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(),
		cfg.SMPP.ConnectTimeout+cfg.SMPP.SubmitTimeout+5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return client.Close()
}
