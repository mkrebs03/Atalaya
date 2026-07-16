package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/mkrebs03/atalaya/internal/server"
)

func TestStableCommandExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantOutput string
		wantError  string
	}{
		{name: "no command", wantCode: exitUsage, wantError: "Usage:"},
		{name: "unknown command", args: []string{"unknown"}, wantCode: exitUsage, wantError: "unknown command"},
		{name: "root short help", args: []string{"-h"}, wantCode: exitSuccess, wantOutput: "Commands:"},
		{name: "root long help", args: []string{"--help"}, wantCode: exitSuccess, wantOutput: "Commands:"},
		{name: "help command", args: []string{"help"}, wantCode: exitSuccess, wantOutput: "Commands:"},
		{name: "help server", args: []string{"help", "server"}, wantCode: exitSuccess, wantOutput: "--public-url"},
		{name: "help unknown", args: []string{"help", "unknown"}, wantCode: exitUsage, wantError: "unknown command"},
		{name: "version", args: []string{"version"}, wantCode: exitSuccess, wantOutput: "atalaya dev\n"},
		{name: "version help", args: []string{"version", "--help"}, wantCode: exitSuccess, wantOutput: "Usage: atalaya version"},
		{name: "version help after argument", args: []string{"version", "extra", "-h"}, wantCode: exitSuccess, wantOutput: "Usage: atalaya version"},
		{name: "version argument", args: []string{"version", "extra"}, wantCode: exitUsage, wantError: "accepts no arguments"},
		{name: "agent reserved", args: []string{"agent"}, wantCode: exitRuntime, wantError: "not implemented"},
		{name: "backup reserved", args: []string{"backup"}, wantCode: exitRuntime, wantError: "not implemented"},
		{name: "verify reserved", args: []string{"verify"}, wantCode: exitRuntime, wantError: "not implemented"},
		{name: "reserved help", args: []string{"agent", "--help"}, wantCode: exitSuccess, wantOutput: "Reserved in Phase 0"},
		{name: "reserved help after argument", args: []string{"backup", "--output", "file", "-h"}, wantCode: exitSuccess, wantOutput: "Reserved in Phase 0"},
		{name: "reserved invalid flag", args: []string{"backup", "--output", "file"}, wantCode: exitUsage, wantError: "unexpected arguments"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			application := testApplication()
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			code := application.Run(test.args, &stdout, &stderr)
			if code != test.wantCode {
				t.Fatalf("Run() code = %d, want %d; stderr=%q", code, test.wantCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), test.wantOutput) {
				t.Errorf("stdout = %q, want containing %q", stdout.String(), test.wantOutput)
			}
			if !strings.Contains(stderr.String(), test.wantError) {
				t.Errorf("stderr = %q, want containing %q", stderr.String(), test.wantError)
			}
		})
	}
}

func TestServerUsesSecureDefaultsAndValidatedConfig(t *testing.T) {
	t.Parallel()

	application := testApplication()
	var received server.Config
	application.runServer = func(_ context.Context, config server.Config, _ http.Handler) error {
		received = config
		return nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := application.Run([]string{"server", "--public-url", "https://atalaya.example.test"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("Run() code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if received.Listen != "127.0.0.1:8080" || received.Mode != server.ModeProduction {
		t.Fatalf("received config = %#v, want secure defaults", received)
	}
}

func TestServerDevelopmentLoopbackStarts(t *testing.T) {
	t.Parallel()

	application := testApplication()
	called := false
	application.runServer = func(_ context.Context, _ server.Config, _ http.Handler) error {
		called = true
		return nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := application.Run([]string{
		"server",
		"--mode", "development",
		"--listen", "127.0.0.1:9090",
		"--public-url", "http://localhost:5173",
	}, &stdout, &stderr)
	if code != exitSuccess || !called {
		t.Fatalf("Run() = %d, called=%v, stderr=%q", code, called, stderr.String())
	}
}

func TestServerConfigurationAndUsageErrorsDoNotStart(t *testing.T) {
	t.Parallel()

	tests := [][]string{
		{"server"},
		{"server", "--unknown"},
		{"server", "--public-url", "http://atalaya.example.test"},
		{"server", "--mode", "development", "--listen", "0.0.0.0:8080", "--public-url", "http://localhost:8080"},
		{"server", "--listen", "0.0.0.0:8080", "--public-url", "https://atalaya.example.test"},
		{"server", "--listen", "atalaya.example.test:8080", "--public-url", "https://atalaya.example.test"},
		{"server", "--public-url", "https://atalaya.example.test", "extra"},
	}
	for _, args := range tests {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			application := testApplication()
			application.runServer = func(context.Context, server.Config, http.Handler) error {
				t.Fatal("server runner called for invalid input")
				return nil
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			if code := application.Run(args, &stdout, &stderr); code != exitUsage {
				t.Fatalf("Run() code = %d, want %d", code, exitUsage)
			}
		})
	}
}

func TestServerRuntimeFailureUsesExitOne(t *testing.T) {
	t.Parallel()

	application := testApplication()
	application.runServer = func(context.Context, server.Config, http.Handler) error {
		return errors.New("listener unavailable")
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := application.Run([]string{"server", "--public-url", "https://atalaya.example.test"}, &stdout, &stderr)
	if code != exitRuntime || !strings.Contains(stderr.String(), "listener unavailable") {
		t.Fatalf("Run() = %d, stderr=%q", code, stderr.String())
	}
}

func TestServerHelpDoesNotStart(t *testing.T) {
	t.Parallel()

	tests := [][]string{
		{"server", "--help"},
		{"server", "--listen", "0.0.0.0:8080", "--help"},
		{"server", "--unknown", "value", "-h"},
	}
	for _, args := range tests {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			application := testApplication()
			application.runServer = func(context.Context, server.Config, http.Handler) error {
				t.Fatal("server runner called for help")
				return nil
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			code := application.Run(args, &stdout, &stderr)
			if code != exitSuccess || !strings.Contains(stdout.String(), "--public-url") {
				t.Fatalf("Run() = %d, stdout=%q, stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestServerConfigurationErrorDoesNotExposeMalformedPublicURL(t *testing.T) {
	t.Parallel()

	secret := "phase-zero-secret"
	application := testApplication()
	application.runServer = func(context.Context, server.Config, http.Handler) error {
		t.Fatal("server runner called for malformed public URL")
		return nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := application.Run([]string{"server", "--public-url", "https://example.test:" + secret}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("Run() = %d, want %d", code, exitUsage)
	}
	if strings.Contains(stderr.String(), secret) {
		t.Fatalf("stderr exposed public URL content: %q", stderr.String())
	}
}

func testApplication() Application {
	return Application{
		version: "dev",
		handler: http.NotFoundHandler(),
		runServer: func(context.Context, server.Config, http.Handler) error {
			return nil
		},
	}
}
