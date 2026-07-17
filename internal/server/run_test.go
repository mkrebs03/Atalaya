package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestRunShutsDownWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- serve(
			ctx,
			time.Second,
			&http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})},
			newBlockingListener(),
		)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not shut down before timeout")
	}
}

func TestRunReportsListenerFailure(t *testing.T) {
	want := fmt.Errorf("listener unavailable")
	err := run(context.Background(), Config{
		Listen:          "127.0.0.1:8080",
		Mode:            ModeProduction,
		PublicURL:       "https://atalaya.example.test",
		ShutdownTimeout: time.Second,
	}, http.NotFoundHandler(), func(network, address string) (net.Listener, error) {
		if network != "tcp" || address != "127.0.0.1:8080" {
			t.Fatalf("listen(%q, %q)", network, address)
		}
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want listener failure", err)
	}
}

func TestRunRejectsConfigurationBeforeOpeningListener(t *testing.T) {
	err := Run(context.Background(), Config{
		Listen:          "0.0.0.0:8080",
		Mode:            ModeProduction,
		PublicURL:       "https://atalaya.example.test",
		ShutdownTimeout: time.Second,
	}, http.NotFoundHandler())
	if err == nil {
		t.Fatal("Run() error = nil")
	}
}

func TestRunDoesNotOpenListenerAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := run(ctx, Config{
		Listen:          "127.0.0.1:8080",
		Mode:            ModeProduction,
		PublicURL:       "https://atalaya.example.test",
		ShutdownTimeout: time.Second,
	}, http.NotFoundHandler(), func(string, string) (net.Listener, error) {
		t.Fatal("listener opened after cancellation")
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

func TestRunRejectsNilHandlerBeforeOpeningListener(t *testing.T) {
	err := run(context.Background(), Config{
		Listen:          "127.0.0.1:8080",
		Mode:            ModeProduction,
		PublicURL:       "https://atalaya.example.test",
		ShutdownTimeout: time.Second,
	}, nil, func(string, string) (net.Listener, error) {
		t.Fatal("listener opened with nil handler")
		return nil, nil
	})
	if err == nil {
		t.Fatal("Run() error = nil")
	}
}

type blockingListener struct {
	closed    chan struct{}
	closeOnce sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{closed: make(chan struct{})}
}

func (listener *blockingListener) Accept() (net.Conn, error) {
	<-listener.closed
	return nil, net.ErrClosed
}

func (listener *blockingListener) Close() error {
	listener.closeOnce.Do(func() { close(listener.closed) })
	return nil
}

func (listener *blockingListener) Addr() net.Addr {
	return testAddress("phase-zero")
}

type testAddress string

func (address testAddress) Network() string { return "test" }
func (address testAddress) String() string  { return string(address) }
