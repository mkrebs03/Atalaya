// Package cli implements Atalaya's stable Phase 0 command-line contract.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/mkrebs03/atalaya/internal/buildinfo"
	"github.com/mkrebs03/atalaya/internal/httpapi"
	"github.com/mkrebs03/atalaya/internal/server"
)

const (
	exitSuccess = 0
	exitRuntime = 1
	exitUsage   = 2
)

// Application owns CLI dependencies that are replaced by behavior tests.
type Application struct {
	version   string
	handler   http.Handler
	runServer func(context.Context, server.Config, http.Handler) error
}

// New creates an application backed by the embedded Vue assets.
func New(assets fs.FS) (Application, error) {
	handler, err := httpapi.New(assets)
	if err != nil {
		return Application{}, err
	}

	return Application{
		version:   buildinfo.Version,
		handler:   handler,
		runServer: server.Run,
	}, nil
}

// Run executes one CLI invocation and returns its process exit code.
func (application Application) Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeRootUsage(stderr)
		return exitUsage
	}

	switch args[0] {
	case "help":
		return application.runHelp(args[1:], stdout, stderr)
	case "version":
		return application.runVersion(args[1:], stdout, stderr)
	case "server":
		return application.runServerCommand(args[1:], stdout, stderr)
	case "agent", "backup", "verify":
		return runReserved(args[0], args[1:], stdout, stderr)
	case "-h", "--help":
		writeRootUsage(stdout)
		return exitSuccess
	default:
		fmt.Fprintf(stderr, "atalaya: unknown command %q\n\n", args[0])
		writeRootUsage(stderr)
		return exitUsage
	}
}

func (application Application) runHelp(args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) {
		writeRootUsage(stdout)
		return exitSuccess
	}
	if len(args) == 0 {
		writeRootUsage(stdout)
		return exitSuccess
	}
	if len(args) > 1 {
		fmt.Fprintln(stderr, "atalaya help: expected at most one command")
		return exitUsage
	}

	switch args[0] {
	case "server":
		writeServerUsage(stdout)
	case "agent", "backup", "verify":
		writeReservedUsage(stdout, args[0])
	case "version":
		writeVersionUsage(stdout)
	case "help":
		fmt.Fprintln(stdout, "Usage: atalaya help [command]")
	default:
		fmt.Fprintf(stderr, "atalaya help: unknown command %q\n", args[0])
		return exitUsage
	}
	return exitSuccess
}

func (application Application) runVersion(args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) {
		writeVersionUsage(stdout)
		return exitSuccess
	}
	if len(args) != 0 {
		fmt.Fprintln(stderr, "atalaya version: this command accepts no arguments")
		return exitUsage
	}

	fmt.Fprintf(stdout, "atalaya %s\n", application.version)
	return exitSuccess
}

func (application Application) runServerCommand(args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) {
		writeServerUsage(stdout)
		return exitSuccess
	}

	flags := flag.NewFlagSet("server", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	listen := flags.String("listen", "127.0.0.1:8080", "TCP loopback address to listen on")
	mode := flags.String("mode", string(server.ModeProduction), "production or development")
	publicURL := flags.String("public-url", "", "public origin used by the browser")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "atalaya server: %v\n", err)
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "atalaya server: unexpected positional arguments")
		return exitUsage
	}

	config := server.Config{
		Listen:          *listen,
		Mode:            server.Mode(*mode),
		PublicURL:       *publicURL,
		ShutdownTimeout: 10 * time.Second,
	}
	if err := config.Validate(); err != nil {
		fmt.Fprintf(stderr, "atalaya server: invalid configuration: %v\n", err)
		return exitUsage
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := application.runServer(ctx, config, application.handler); err != nil {
		fmt.Fprintf(stderr, "atalaya server: %v\n", err)
		return exitRuntime
	}
	return exitSuccess
}

func runReserved(command string, args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) {
		writeReservedUsage(stdout, command)
		return exitSuccess
	}
	if len(args) != 0 {
		fmt.Fprintf(stderr, "atalaya %s: unexpected arguments\n", command)
		return exitUsage
	}

	fmt.Fprintf(stderr, "atalaya %s: command is reserved but not implemented in Phase 0\n", command)
	return exitRuntime
}

func containsHelpFlag(args []string) bool {
	for _, argument := range args {
		if argument == "-h" || argument == "--help" {
			return true
		}
	}
	return false
}
