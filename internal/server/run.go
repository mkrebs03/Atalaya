package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// Run serves HTTP until the context is cancelled or the listener fails.
func Run(ctx context.Context, config Config, handler http.Handler) error {
	listener, err := net.Listen("tcp", config.Listen)
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
