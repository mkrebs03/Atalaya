package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Run serves HTTP until the context is cancelled or the listener fails.
func Run(ctx context.Context, config Config, handler http.Handler) error {
	return run(ctx, config, handler, net.Listen)
}

func run(
	ctx context.Context,
	config Config,
	handler http.Handler,
	listen func(string, string) (net.Listener, error),
) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid server configuration: %w", err)
	}
	if handler == nil {
		return fmt.Errorf("HTTP handler is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	listener, err := listen("tcp", config.Listen)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              config.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return serve(ctx, config.ShutdownTimeout, httpServer, listener)
}

func serve(ctx context.Context, shutdownTimeout time.Duration, httpServer *http.Server, listener net.Listener) error {
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- httpServer.Serve(listener)
	}()

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := httpServer.Shutdown(shutdownContext); err != nil {
			_ = httpServer.Close()
			return err
		}

		err := <-serveErrors
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
