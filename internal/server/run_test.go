package server

import (
	"context"
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
	err := Run(context.Background(), Config{
		Listen:          "missing-port",
		ShutdownTimeout: time.Second,
	}, http.NotFoundHandler())
	if err == nil {
		t.Fatal("Run() error = nil, want listener failure")
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
