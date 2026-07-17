// Package cli implements Atalaya's stable command-line contract.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mkrebs03/atalaya/internal/auth"
	"github.com/mkrebs03/atalaya/internal/buildinfo"
	startupconfig "github.com/mkrebs03/atalaya/internal/config"
	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
	"github.com/mkrebs03/atalaya/internal/database"
	"github.com/mkrebs03/atalaya/internal/httpapi"
	"github.com/mkrebs03/atalaya/internal/server"
)

const (
	exitSuccess = 0
	exitRuntime = 1
	exitUsage   = 2

	defaultServerConfigPath = "/etc/atalaya/server.yaml"
)

type databaseHandle interface {
	auth.AdministratorRepository
	Close() error
}

// Application owns CLI dependencies that are replaced by behavior tests.
type Application struct {
	version           string
	newHandler        func() (http.Handler, error)
	runServer         func(context.Context, server.Config, http.Handler) error
	loadConfig        func(string) (startupconfig.ServerConfig, error)
	loadMasterKey     func(string) ([]byte, error)
	preflightDatabase func(context.Context, string, []byte) (database.PreflightResult, error)
	openDatabase      func(context.Context, string, []byte) (databaseHandle, error)
	readPassword      passwordReader
	hashPassword      passwordHasher
	assets            fs.FS
}

// New creates an application backed by the embedded Vue assets.
func New(assets fs.FS) (Application, error) {
	if assets == nil {
		return Application{}, fmt.Errorf("embedded frontend assets are required")
	}
	return Application{
		version: buildinfo.Version,
		assets:  assets,
		newHandler: func() (http.Handler, error) {
			return httpapi.New(assets)
		},
		runServer:         server.Run,
		loadConfig:        startupconfig.LoadServer,
		loadMasterKey:     atalayacrypto.LoadMasterKey,
		preflightDatabase: database.Preflight,
		openDatabase: func(ctx context.Context, path string, key []byte) (databaseHandle, error) {
			return database.Open(ctx, path, key)
		},
		readPassword: func(prompt string, output io.Writer) ([]byte, error) {
			return readTerminalPassword(os.Stdin, output, prompt)
		},
		hashPassword: auth.HashPassword,
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
	case "migrate":
		return application.runMigrateCommand(args[1:], stdout, stderr)
	case "admin":
		return application.runAdminCommand(args[1:], stdout, stderr)
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
	case "migrate":
		writeMigrateUsage(stdout)
	case "admin":
		writeAdminUsage(stdout)
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
	configPath, code, ok := parseConfigOnlyFlags("server", args, stderr)
	if !ok {
		return code
	}
	loaded, key, code, ok := application.loadBootstrap("server", configPath, stderr)
	if !ok {
		return code
	}
	defer clear(key)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	owner, err := application.openDatabase(ctx, loaded.DatabasePath, key)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya server: %v\n", err)
		return exitRuntime
	}
	clear(key)

	handler, err := application.newHandler()
	if repository, ok := owner.(auth.Repository); ok && application.assets != nil {
		service, serviceErr := auth.NewService(repository)
		if serviceErr != nil {
			_ = owner.Close()
			fmt.Fprintf(stderr, "atalaya server: construct authentication service: %v\n", serviceErr)
			return exitRuntime
		}
		handler, err = httpapi.NewAuthenticated(application.assets, service, httpapi.AuthConfig{Listen: loaded.Listen, Mode: loaded.Mode, PublicURL: loaded.PublicURL, TrustedProxy: loaded.TrustedProxy})
	}
	if err != nil {
		_ = owner.Close()
		fmt.Fprintf(stderr, "atalaya server: construct HTTP handler: %v\n", err)
		return exitRuntime
	}
	processConfig := server.Config{
		Listen:          loaded.Listen,
		Mode:            loaded.Mode,
		PublicURL:       loaded.PublicURL,
		TrustedProxy:    loaded.TrustedProxy,
		ShutdownTimeout: 10 * time.Second,
	}
	runErr := application.runServer(ctx, processConfig, handler)
	closeErr := owner.Close()
	if runErr != nil {
		fmt.Fprintf(stderr, "atalaya server: %v\n", runErr)
		return exitRuntime
	}
	if closeErr != nil {
		fmt.Fprintf(stderr, "atalaya server: close database: %v\n", closeErr)
		return exitRuntime
	}
	return exitSuccess
}

func (application Application) runMigrateCommand(args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) {
		writeMigrateUsage(stdout)
		return exitSuccess
	}
	configPath, code, ok := parseConfigOnlyFlags("migrate", args, stderr)
	if !ok {
		return code
	}
	loaded, key, code, ok := application.loadBootstrap("migrate", configPath, stderr)
	if !ok {
		return code
	}
	defer clear(key)

	owner, err := application.openDatabase(context.Background(), loaded.DatabasePath, key)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya migrate: %v\n", err)
		return exitRuntime
	}
	clear(key)
	if err := owner.Close(); err != nil {
		fmt.Fprintf(stderr, "atalaya migrate: close database: %v\n", err)
		return exitRuntime
	}
	fmt.Fprintln(stdout, "database migrations applied")
	return exitSuccess
}

func (application Application) runAdminCommand(args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) {
		if len(args) > 0 && args[0] == "create" {
			writeAdminCreateUsage(stdout)
		} else if len(args) > 0 && args[0] == "reset-password" {
			writeAdminResetPasswordUsage(stdout)
		} else {
			writeAdminUsage(stdout)
		}
		return exitSuccess
	}
	if len(args) == 0 {
		writeAdminUsage(stderr)
		return exitUsage
	}
	if args[0] == "reset-password" {
		return application.runAdminResetPasswordCommand(args[1:], stdout, stderr)
	}
	if args[0] != "create" {
		fmt.Fprintf(stderr, "atalaya admin: unknown command %q\n", args[0])
		return exitUsage
	}
	return application.runAdminCreateCommand(args[1:], stdout, stderr)
}

func (application Application) runAdminResetPasswordCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("admin reset-password", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultServerConfigPath, "server YAML configuration path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *configPath == "" {
		if err != nil {
			fmt.Fprintf(stderr, "atalaya admin reset-password: %v\n", err)
		} else {
			fmt.Fprintln(stderr, "atalaya admin reset-password: invalid arguments")
		}
		return exitUsage
	}
	loaded, key, code, ok := application.loadBootstrap("admin reset-password", *configPath, stderr)
	if !ok {
		return code
	}
	defer clear(key)
	preflight, err := application.preflightDatabase(context.Background(), loaded.DatabasePath, key)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya admin reset-password: %v\n", err)
		return exitRuntime
	}
	if !preflight.HasActiveAdministrator {
		fmt.Fprintln(stderr, "atalaya admin reset-password: active administrator is required")
		return exitRuntime
	}
	passwordHash, err := readConfirmedPasswordHash(application.readPassword, application.hashPassword, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya admin reset-password: %v\n", err)
		return exitRuntime
	}
	owner, err := application.openDatabase(context.Background(), loaded.DatabasePath, key)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya admin reset-password: %v\n", err)
		return exitRuntime
	}
	clear(key)
	repository, ok := owner.(auth.Repository)
	if !ok {
		_ = owner.Close()
		fmt.Fprintln(stderr, "atalaya admin reset-password: database does not support authentication")
		return exitRuntime
	}
	service, err := auth.NewService(repository)
	if err == nil {
		err = service.ResetPassword(context.Background(), passwordHash)
	}
	closeErr := owner.Close()
	if err != nil {
		fmt.Fprintf(stderr, "atalaya admin reset-password: %v\n", err)
		return exitRuntime
	}
	if closeErr != nil {
		fmt.Fprintf(stderr, "atalaya admin reset-password: close database: %v\n", closeErr)
		return exitRuntime
	}
	fmt.Fprintln(stdout, "administrator password reset; all sessions revoked")
	return exitSuccess
}

func (application Application) runAdminCreateCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("admin create", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultServerConfigPath, "server YAML configuration path")
	username := flags.String("username", "admin", "administrator username")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "atalaya admin create: %v\n", err)
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "atalaya admin create: unexpected positional arguments")
		return exitUsage
	}
	if *configPath == "" {
		fmt.Fprintln(stderr, "atalaya admin create: config path must not be empty")
		return exitUsage
	}
	normalizedUsername, err := auth.NormalizeUsername(*username)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya admin create: invalid username: %v\n", err)
		return exitUsage
	}

	loaded, key, code, ok := application.loadBootstrap("admin create", *configPath, stderr)
	if !ok {
		return code
	}
	defer clear(key)
	preflight, err := application.preflightDatabase(context.Background(), loaded.DatabasePath, key)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya admin create: %v\n", err)
		return exitRuntime
	}
	if preflight.HasActiveAdministrator {
		fmt.Fprintf(stderr, "atalaya admin create: %v\n", database.ErrAdministratorExists)
		return exitRuntime
	}

	passwordHash, err := readConfirmedPasswordHash(application.readPassword, application.hashPassword, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya admin create: %v\n", err)
		return exitRuntime
	}
	owner, err := application.openDatabase(context.Background(), loaded.DatabasePath, key)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya admin create: %v\n", err)
		return exitRuntime
	}
	clear(key)
	creator := auth.NewAdministratorCreator(owner)
	_, createErr := creator.Create(context.Background(), normalizedUsername, passwordHash)
	closeErr := owner.Close()
	if createErr != nil {
		fmt.Fprintf(stderr, "atalaya admin create: %v\n", createErr)
		return exitRuntime
	}
	if closeErr != nil {
		fmt.Fprintf(stderr, "atalaya admin create: close database: %v\n", closeErr)
		return exitRuntime
	}
	fmt.Fprintf(stdout, "administrator %q created\n", normalizedUsername)
	return exitSuccess
}

func (application Application) loadBootstrap(
	command string,
	configPath string,
	stderr io.Writer,
) (startupconfig.ServerConfig, []byte, int, bool) {
	loaded, err := application.loadConfig(configPath)
	if err != nil {
		if errors.Is(err, startupconfig.ErrInvalidServerConfig) {
			fmt.Fprintf(stderr, "atalaya %s: %v\n", command, err)
			return startupconfig.ServerConfig{}, nil, exitUsage, false
		}
		fmt.Fprintf(stderr, "atalaya %s: %v\n", command, err)
		return startupconfig.ServerConfig{}, nil, exitRuntime, false
	}
	key, err := application.loadMasterKey(loaded.MasterKeyPath)
	if err != nil {
		clear(key)
		fmt.Fprintf(stderr, "atalaya %s: %v\n", command, err)
		return startupconfig.ServerConfig{}, nil, exitRuntime, false
	}
	return loaded, key, exitSuccess, true
}

func parseConfigOnlyFlags(command string, args []string, stderr io.Writer) (string, int, bool) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultServerConfigPath, "server YAML configuration path")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "atalaya %s: %v\n", command, err)
		return "", exitUsage, false
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "atalaya %s: unexpected positional arguments\n", command)
		return "", exitUsage, false
	}
	if *configPath == "" {
		fmt.Fprintf(stderr, "atalaya %s: config path must not be empty\n", command)
		return "", exitUsage, false
	}
	return *configPath, exitSuccess, true
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

	fmt.Fprintf(stderr, "atalaya %s: command is reserved but not implemented\n", command)
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
