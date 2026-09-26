// Command sqs-to-smpp-gateway receives the queue messages pushed over HTTP
// (e.g. by a Scaleway Queues trigger) and sends them as SMS, either to an
// SMSC over SMPP or through the HTTP API of a provider (Twilio, OVH,
// ClickSend).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/clicksend"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/config"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/handler"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/ovh"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/smpp"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/twilio"
)

// provider sends the SMS. Close releases its connections, after the
// in-flight sends.
type provider interface {
	handler.Sender
	Close() error
}

func main() {
	if err := run(); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	client, err := newProvider(cfg, logger)
	if err != nil {
		return err
	}
	logger.Info("sms provider selected", "protocol", cfg.Protocol, "provider", cfg.Provider)

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
	// trigger, which may send the SMS twice), then unbind from the SMSC.
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.SendTimeout()+5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return client.Close()
}

func newProvider(cfg config.Config, logger *slog.Logger) (provider, error) {
	switch cfg.Protocol {
	case config.ProtocolSMPP:
		return smpp.NewClient(cfg.SMPP, logger), nil
	case config.ProtocolHTTP:
		switch cfg.Provider {
		case config.ProviderTwilio:
			return twilio.NewClient(cfg.Twilio), nil
		case config.ProviderOVH:
			return ovh.NewClient(cfg.OVH), nil
		case config.ProviderClickSend:
			return clicksend.NewClient(cfg.ClickSend), nil
		}
	}
	return nil, fmt.Errorf("no provider %q for protocol %q", cfg.Provider, cfg.Protocol)
}
