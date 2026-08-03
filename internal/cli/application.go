// Package cli implements Atalaya's stable command-line contract.
package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mkrebs03/atalaya/internal/agent"
	"github.com/mkrebs03/atalaya/internal/auth"
	"github.com/mkrebs03/atalaya/internal/backup"
	"github.com/mkrebs03/atalaya/internal/buildinfo"
	"github.com/mkrebs03/atalaya/internal/checks"
	"github.com/mkrebs03/atalaya/internal/collection"
	startupconfig "github.com/mkrebs03/atalaya/internal/config"
	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
	"github.com/mkrebs03/atalaya/internal/database"
	"github.com/mkrebs03/atalaya/internal/hostquery"
	"github.com/mkrebs03/atalaya/internal/hosts"
	"github.com/mkrebs03/atalaya/internal/httpapi"
	metricdomain "github.com/mkrebs03/atalaya/internal/metrics"
	"github.com/mkrebs03/atalaya/internal/notifications"
	"github.com/mkrebs03/atalaya/internal/retention"
	"github.com/mkrebs03/atalaya/internal/server"
	"github.com/mkrebs03/atalaya/internal/tailscale"
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
	version                  string
	newHandler               func() (http.Handler, error)
	runServer                func(context.Context, server.Config, http.Handler) error
	loadConfig               func(string) (startupconfig.ServerConfig, error)
	loadMasterKey            func(string) ([]byte, error)
	preflightDatabase        func(context.Context, string, []byte) (database.PreflightResult, error)
	openDatabase             func(context.Context, string, []byte) (databaseHandle, error)
	createBackup             func(context.Context, backup.Options) (backup.Manifest, error)
	verifyDatabase           func(context.Context, string, []byte) (database.Verification, error)
	exportMasterKey          func(string, string) error
	confirm                  func(io.Writer) (bool, error)
	configureTailscale       func(context.Context) (tailscale.Result, error)
	configureTailscaleRoutes func(context.Context, map[int]string) (tailscale.Result, error)
	readPassword             passwordReader
	hashPassword             passwordHasher
	assets                   fs.FS
	loadAgentConfig          func(string) (startupconfig.AgentConfig, error)
	loadCredential           func(string) ([]byte, error)
	runAgent                 func(context.Context, string, http.Handler) error
	newCollector             func() agent.Collector
	runCollection            func(context.Context, collection.Repository) error
	runChecks                func(context.Context, checks.SchedulerRepository, checks.ManualRepository, checks.Executor) error
	runNotifications         func(context.Context, notifications.Repository) error
	random                   io.Reader
}

// New creates an application backed by the embedded Vue assets.
func New(assets fs.FS) (Application, error) {
	if assets == nil {
		return Application{}, fmt.Errorf("embedded frontend assets are required")
	}
	return Application{
		version:         buildinfo.Version,
		assets:          assets,
		loadAgentConfig: startupconfig.LoadAgent,
		loadCredential:  agent.LoadCredential,
		runAgent:        agent.Run,
		newCollector:    func() agent.Collector { return agent.NewLinuxCollector() },
		runCollection: func(ctx context.Context, repository collection.Repository) error {
			client, err := collection.NewClient(nil, nil, nil)
			if err != nil {
				return err
			}
			scheduler := func(runContext context.Context) error {
				return (collection.Scheduler{Repository: repository, Puller: client}).Run(runContext)
			}
			jobs := []func(context.Context) error{scheduler}
			if retentionRepository, ok := repository.(metricdomain.RetentionRepository); ok {
				worker := retention.Worker{Repository: retentionRepository}
				jobs = append(jobs, worker.Run)
			}
			return runConcurrentJobs(ctx, jobs...)
		},
		runChecks: func(ctx context.Context, schedulerRepository checks.SchedulerRepository, manualRepository checks.ManualRepository, executor checks.Executor) error {
			return runConcurrentJobs(ctx,
				func(runContext context.Context) error {
					return (checks.Scheduler{Repository: schedulerRepository, Probe: executor, Concurrency: 2}).Run(runContext)
				},
				func(runContext context.Context) error {
					return (checks.ManualWorker{Repository: manualRepository, Probe: executor, Concurrency: 2}).Run(runContext)
				},
			)
		},
		runNotifications: func(ctx context.Context, repository notifications.Repository) error {
			return (notifications.Worker{Repository: repository, Sender: notifications.DiscordSender{}}).Run(ctx)
		},
		newHandler: func() (http.Handler, error) {
			return httpapi.New(assets)
		},
		runServer:          server.Run,
		loadConfig:         startupconfig.LoadServer,
		loadMasterKey:      atalayacrypto.LoadMasterKey,
		createBackup:       backup.Create,
		verifyDatabase:     database.Verify,
		exportMasterKey:    backup.ExportKey,
		confirm:            func(output io.Writer) (bool, error) { return confirmExport(os.Stdin, output) },
		configureTailscale: func(ctx context.Context) (tailscale.Result, error) { return tailscale.New().Configure(ctx) },
		configureTailscaleRoutes: func(ctx context.Context, routes map[int]string) (tailscale.Result, error) {
			return tailscale.New().ConfigureRoutes(ctx, routes)
		},
		preflightDatabase: database.Preflight,
		openDatabase: func(ctx context.Context, path string, key []byte) (databaseHandle, error) {
			return database.Open(ctx, path, key)
		},
		readPassword: func(prompt string, output io.Writer) ([]byte, error) {
			return readTerminalPassword(os.Stdin, output, prompt)
		},
		hashPassword: auth.HashPassword,
		random:       rand.Reader,
	}, nil
}

func runConcurrentJobs(ctx context.Context, jobs ...func(context.Context) error) error {
	if len(jobs) == 0 {
		return nil
	}
	coordinated, cancel := context.WithCancel(ctx)
	defer cancel()
	errorsChannel := make(chan error, len(jobs))
	for _, job := range jobs {
		go func(run func(context.Context) error) { errorsChannel <- run(coordinated) }(job)
	}
	var firstErr error
	for range jobs {
		err := <-errorsChannel
		if err != nil && !errors.Is(err, context.Canceled) && firstErr == nil {
			firstErr = err
			cancel()
		}
	}
	return firstErr
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
	case "agent":
		return application.runAgentCommand(args[1:], stdout, stderr)
	case "bootstrap":
		return application.runBootstrapCommand(args[1:], stdout, stderr)
	case "backup":
		return application.runBackupCommand(args[1:], stdout, stderr)
	case "backup-key":
		return application.runBackupKeyCommand(args[1:], stdout, stderr)
	case "verify":
		return application.runVerifyCommand(args[1:], stdout, stderr)
	case "tailscale":
		return application.runTailscaleCommand(args[1:], stdout, stderr)
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
	case "agent":
		writeAgentUsage(stdout)
	case "bootstrap":
		writeBootstrapUsage(stdout)
	case "backup":
		writeBackupUsage(stdout)
	case "backup-key":
		writeBackupKeyUsage(stdout)
	case "verify":
		writeVerifyUsage(stdout)
	case "tailscale":
		writeTailscaleUsage(stdout)
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

func (application Application) runBootstrapCommand(args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) && (len(args) == 0 || args[0] != "local-agent") {
		writeBootstrapUsage(stdout)
		return exitSuccess
	}
	if len(args) == 0 || args[0] != "local-agent" {
		if len(args) == 0 {
			writeBootstrapUsage(stderr)
		} else {
			fmt.Fprintf(stderr, "atalaya bootstrap: unknown command %q\n", args[0])
		}
		return exitUsage
	}
	return application.runLocalAgentBootstrap(args[1:], stdout, stderr)
}

func (application Application) runLocalAgentBootstrap(args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) {
		writeBootstrapLocalAgentUsage(stdout)
		return exitSuccess
	}
	flags := flag.NewFlagSet("bootstrap local-agent", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	serverConfig := flags.String("server-config", "", "server YAML configuration path")
	agentConfig := flags.String("agent-config", "", "agent YAML configuration path")
	agentURL := flags.String("agent-url", "", "advertised agent URL")
	displayName := flags.String("display-name", hosts.DefaultDisplayName, "host display name")
	listen := flags.String("listen", hosts.DefaultListen, "agent listen address")
	credentialPath := flags.String("credential-path", "", "credential path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *serverConfig == "" || *agentConfig == "" || *agentURL == "" {
		if err != nil {
			fmt.Fprintf(stderr, "atalaya bootstrap local-agent: %v\n", err)
		} else {
			fmt.Fprintln(stderr, "atalaya bootstrap local-agent: --server-config, --agent-config, and --agent-url are required")
		}
		return exitUsage
	}
	loaded, key, code, ok := application.loadBootstrap("bootstrap local-agent", *serverConfig, stderr)
	if !ok {
		return code
	}
	defer clear(key)
	owner, err := application.openDatabase(context.Background(), loaded.DatabasePath, key)
	clear(key)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya bootstrap local-agent: %v\n", err)
		return exitRuntime
	}
	request := hosts.BootstrapRequest{DisplayName: *displayName, Listen: *listen, ServerURL: loaded.PublicURL, AgentURL: *agentURL, AgentConfig: *agentConfig, CredentialPath: *credentialPath}
	repository, ok := owner.(hosts.LocalHostRepository)
	if !ok {
		_ = owner.Close()
		fmt.Fprintln(stderr, "atalaya bootstrap local-agent: database does not support local hosts")
		return exitRuntime
	}
	hostID, runErr := (hosts.Bootstrapper{Repository: repository}).Run(context.Background(), request)
	closeErr := owner.Close()
	if runErr != nil {
		fmt.Fprintf(stderr, "atalaya bootstrap local-agent: %v\n", runErr)
		if errors.Is(runErr, hosts.ErrInvalidBootstrapRequest) {
			return exitUsage
		}
		return exitRuntime
	}
	if closeErr != nil {
		fmt.Fprintf(stderr, "atalaya bootstrap local-agent: close database: %v\n", closeErr)
		return exitRuntime
	}
	fmt.Fprintf(stdout, "local agent bootstrapped: %s\n", hostID)
	return exitSuccess
}

func (application Application) runAgentCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "enroll" {
		return application.runAgentEnrollCommand(args[1:], stdout, stderr)
	}
	if containsHelpFlag(args) {
		writeAgentUsage(stdout)
		return exitSuccess
	}
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "agent YAML configuration path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *configPath == "" {
		if err != nil {
			fmt.Fprintf(stderr, "atalaya agent: %v\n", err)
		} else {
			fmt.Fprintln(stderr, "atalaya agent: --config is required")
		}
		return exitUsage
	}
	loaded, err := application.loadAgentConfig(*configPath)
	if err != nil {
		if errors.Is(err, startupconfig.ErrInvalidAgentConfig) {
			fmt.Fprintf(stderr, "atalaya agent: %v\n", err)
			return exitUsage
		}
		fmt.Fprintf(stderr, "atalaya agent: %v\n", err)
		return exitRuntime
	}
	credential, err := application.loadCredential(loaded.CredentialPath)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya agent: %v\n", err)
		return exitRuntime
	}
	defer clear(credential)
	handler, err := agent.NewHandler(credential, application.version, application.newCollector())
	if err != nil {
		fmt.Fprintf(stderr, "atalaya agent: %v\n", err)
		return exitRuntime
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := application.runAgent(ctx, loaded.Listen, handler); err != nil {
		fmt.Fprintf(stderr, "atalaya agent: %v\n", err)
		return exitRuntime
	}
	return exitSuccess
}

func (application Application) runAgentEnrollCommand(args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) {
		writeAgentEnrollUsage(stdout)
		return exitSuccess
	}
	flags := flag.NewFlagSet("agent enroll", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "agent YAML configuration path")
	displayName := flags.String("display-name", "", "host display name")
	tokenFile := flags.String("token-file", "", "0600 enrollment-token file (default: hidden prompt)")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *configPath == "" {
		if err != nil {
			fmt.Fprintf(stderr, "atalaya agent enroll: %v\n", err)
		} else {
			fmt.Fprintln(stderr, "atalaya agent enroll: --config is required")
		}
		return exitUsage
	}
	loaded, err := application.loadAgentConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya agent enroll: %v\n", err)
		if errors.Is(err, startupconfig.ErrInvalidAgentConfig) {
			return exitUsage
		}
		return exitRuntime
	}
	name := *displayName
	if name == "" {
		name, err = os.Hostname()
		if err != nil || name == "" {
			name = "remote-agent"
		}
	}
	var secret []byte
	if *tokenFile != "" {
		secret, err = readEnrollmentTokenFile(*tokenFile)
		removeErr := os.Remove(*tokenFile)
		if err == nil && removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = fmt.Errorf("remove enrollment token file: %w", removeErr)
		}
	} else {
		secret, err = application.readPassword("Enrollment token: ", stderr)
	}
	if err != nil {
		fmt.Fprintf(stderr, "atalaya agent enroll: %v\n", err)
		return exitRuntime
	}
	defer clear(secret)
	if len(secret) == 0 {
		fmt.Fprintln(stderr, "atalaya agent enroll: enrollment token is required")
		return exitUsage
	}
	if application.random == nil {
		application.random = rand.Reader
	}
	hostID, err := agent.Enroll(context.Background(), agent.EnrollmentOptions{Config: loaded, DisplayName: name, Token: string(secret), Version: application.version, Random: application.random})
	if err != nil {
		fmt.Fprintf(stderr, "atalaya agent enroll: %v\n", err)
		return exitRuntime
	}
	fmt.Fprintf(stdout, "agent enrolled: %s\n", hostID)
	return exitSuccess
}

func readEnrollmentTokenFile(path string) ([]byte, error) {
	file, err := os.Open(path) //nolint:gosec // path comes from a --token-file CLI flag set by the operator
	if err != nil {
		return nil, fmt.Errorf("open enrollment token file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("enrollment token file must be a regular file with mode 0600")
	}
	value, err := io.ReadAll(io.LimitReader(file, 128))
	if err != nil {
		return nil, err
	}
	if len(value) == 44 && value[43] == '\n' {
		value = value[:43]
	}
	if len(value) != 43 {
		clear(value)
		return nil, fmt.Errorf("enrollment token file has invalid encoding")
	}
	return value, nil
}

func (application Application) runBackupCommand(args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) {
		writeBackupUsage(stdout)
		return exitSuccess
	}
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultServerConfigPath, "server YAML configuration path")
	output := flags.String("output", "", "0600 data backup output path")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "atalaya backup: %v\n", err)
		return exitUsage
	}
	if flags.NArg() != 0 || *configPath == "" || *output == "" {
		fmt.Fprintln(stderr, "atalaya backup: --config and --output are required and positional arguments are not allowed")
		return exitUsage
	}
	loaded, key, code, ok := application.loadBootstrap("backup", *configPath, stderr)
	if !ok {
		return code
	}
	defer clear(key)
	create := application.createBackup
	if create == nil {
		create = backup.Create
	}
	_, err := create(context.Background(), backup.Options{ConfigPath: *configPath, Config: loaded, Key: key, Output: *output, Version: application.version})
	if err != nil {
		fmt.Fprintf(stderr, "atalaya backup: %v\n", err)
		return exitRuntime
	}
	fmt.Fprintln(stdout, "backup created")
	return exitSuccess
}

func (application Application) runBackupKeyCommand(args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) {
		writeBackupKeyUsage(stdout)
		return exitSuccess
	}
	flags := flag.NewFlagSet("backup-key", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultServerConfigPath, "server YAML configuration path")
	output := flags.String("output", "", "0600 master-key export path")
	yes := flags.Bool("yes", false, "confirm non-interactively")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "atalaya backup-key: %v\n", err)
		return exitUsage
	}
	if flags.NArg() != 0 || *configPath == "" || *output == "" {
		fmt.Fprintln(stderr, "atalaya backup-key: --config and --output are required and positional arguments are not allowed")
		return exitUsage
	}
	loaded, key, code, ok := application.loadBootstrap("backup-key", *configPath, stderr)
	if !ok {
		return code
	}
	defer clear(key)
	if !*yes {
		if application.confirm == nil {
			fmt.Fprintln(stderr, "atalaya backup-key: interactive confirmation is unavailable; use --yes")
			return exitRuntime
		}
		confirmed, err := application.confirm(stderr)
		if err != nil {
			fmt.Fprintf(stderr, "atalaya backup-key: confirmation failed: %v\n", err)
			return exitRuntime
		}
		if !confirmed {
			fmt.Fprintln(stderr, "atalaya backup-key: export cancelled")
			return exitRuntime
		}
	}
	export := application.exportMasterKey
	if export == nil {
		export = backup.ExportKey
	}
	if err := export(loaded.MasterKeyPath, *output); err != nil {
		fmt.Fprintf(stderr, "atalaya backup-key: %v\n", err)
		return exitRuntime
	}
	fmt.Fprintln(stdout, "master key exported")
	return exitSuccess
}

func (application Application) runVerifyCommand(args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) {
		writeVerifyUsage(stdout)
		return exitSuccess
	}
	configPath, code, ok := parseConfigOnlyFlags("verify", args, stderr)
	if !ok {
		return code
	}
	loaded, key, code, ok := application.loadBootstrap("verify", configPath, stderr)
	if !ok {
		return code
	}
	defer clear(key)
	verify := application.verifyDatabase
	if verify == nil {
		verify = database.Verify
	}
	result, err := verify(context.Background(), loaded.DatabasePath, key)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya verify: %v\n", err)
		return exitRuntime
	}
	administrator := "absent"
	if result.HasActiveAdministrator {
		administrator = "present"
	}
	fmt.Fprintf(stdout, "verified schema=%d database_size_bytes=%d administrator=%s\n", result.SchemaVersion, result.DatabaseSizeBytes, administrator)
	return exitSuccess
}

func (application Application) runTailscaleCommand(args []string, stdout, stderr io.Writer) int {
	if containsHelpFlag(args) {
		writeTailscaleUsage(stdout)
		return exitSuccess
	}
	if len(args) == 0 || args[0] != "serve" {
		fmt.Fprintln(stderr, "atalaya tailscale: expected subcommand serve")
		return exitUsage
	}
	flags := flag.NewFlagSet("tailscale serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	role := flags.String("role", "both", "routes to configure: both, server, or agent")
	serverConfig := flags.String("server-config", "", "server YAML configuration path")
	agentConfig := flags.String("agent-config", "", "agent YAML configuration path")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		if err != nil {
			fmt.Fprintf(stderr, "atalaya tailscale serve: %v\n", err)
		}
		return exitUsage
	}
	routes, err := configuredTailscaleRoutes(*role, *serverConfig, *agentConfig)
	if err != nil {
		fmt.Fprintf(stderr, "atalaya tailscale serve: %v\n", err)
		return exitUsage
	}
	configure := application.configureTailscale
	if routes != nil {
		configureRoutes := application.configureTailscaleRoutes
		if configureRoutes == nil {
			configureRoutes = func(ctx context.Context, desired map[int]string) (tailscale.Result, error) {
				return tailscale.New().ConfigureRoutes(ctx, desired)
			}
		}
		result, err := configureRoutes(context.Background(), routes)
		if err != nil {
			fmt.Fprintf(stderr, "atalaya tailscale serve: %v\n", err)
			return exitRuntime
		}
		fmt.Fprintf(stdout, "tailscale Serve verified: %s (%s)\n", result.DNSName, result.Version)
		return exitSuccess
	}
	if configure == nil {
		configure = func(ctx context.Context) (tailscale.Result, error) { return tailscale.New().Configure(ctx) }
	}
	result, err := configure(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "atalaya tailscale serve: %v\n", err)
		return exitRuntime
	}
	fmt.Fprintf(stdout, "tailscale Serve verified: %s (%s)\n", result.DNSName, result.Version)
	return exitSuccess
}

func configuredTailscaleRoutes(role, serverConfigPath, agentConfigPath string) (map[int]string, error) {
	switch role {
	case "both":
		if serverConfigPath == "" && agentConfigPath == "" {
			return nil, nil
		}
		return nil, fmt.Errorf("role both requires no configuration paths; use --role server or --role agent")
	case "server":
		if serverConfigPath == "" {
			serverConfigPath = defaultServerConfigPath
		}
		serverConfig, err := startupconfig.LoadServer(serverConfigPath)
		if err != nil {
			return nil, fmt.Errorf("load server configuration: %w", err)
		}
		serverTarget, err := localHTTPProxyTarget(serverConfig.Listen)
		if err != nil {
			return nil, fmt.Errorf("derive server Serve target: %w", err)
		}
		routes := map[int]string{tailscale.DashboardPort: serverTarget}
		if agentConfigPath == "" {
			routes[tailscale.AgentPort] = tailscale.AgentTarget
			return routes, nil
		}
		agentConfig, err := startupconfig.LoadAgent(agentConfigPath)
		if err != nil {
			return nil, fmt.Errorf("load agent configuration: %w", err)
		}
		agentTarget, err := localHTTPProxyTarget(agentConfig.Listen)
		if err != nil {
			return nil, fmt.Errorf("derive agent Serve target: %w", err)
		}
		routes[tailscale.AgentPort] = agentTarget
		return routes, nil
	case "agent":
		if agentConfigPath == "" {
			return nil, fmt.Errorf("--agent-config is required for agent role")
		}
		agentConfig, err := startupconfig.LoadAgent(agentConfigPath)
		if err != nil {
			return nil, fmt.Errorf("load agent configuration: %w", err)
		}
		agentTarget, err := localHTTPProxyTarget(agentConfig.Listen)
		if err != nil {
			return nil, fmt.Errorf("derive agent Serve target: %w", err)
		}
		return map[int]string{tailscale.AgentPort: agentTarget}, nil
	default:
		return nil, fmt.Errorf("unsupported Serve role %q", role)
	}
}

func localHTTPProxyTarget(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil || host == "" || port == "" {
		return "", fmt.Errorf("listen address %q is invalid", listen)
	}
	return "http://" + net.JoinHostPort(host, port), nil
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
		if hostRepository, available := owner.(hostquery.Repository); available {
			handler, err = httpapi.NewAuthenticated(application.assets, service, httpapi.AuthConfig{Listen: loaded.Listen, Mode: loaded.Mode, PublicURL: loaded.PublicURL, TrustedProxy: loaded.TrustedProxy, Version: application.version}, hostRepository)
		} else {
			handler, err = httpapi.NewAuthenticated(application.assets, service, httpapi.AuthConfig{Listen: loaded.Listen, Mode: loaded.Mode, PublicURL: loaded.PublicURL, TrustedProxy: loaded.TrustedProxy, Version: application.version})
		}
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
	jobs := make([]func(context.Context) error, 0, 3)
	if repository, ok := owner.(collection.Repository); ok && application.runCollection != nil {
		jobs = append(jobs, func(runContext context.Context) error { return application.runCollection(runContext, repository) })
	}
	if schedulerRepository, ok := owner.(checks.SchedulerRepository); ok && application.runChecks != nil {
		manualRepository, manualOK := owner.(checks.ManualRepository)
		if !manualOK {
			_ = owner.Close()
			fmt.Fprintln(stderr, "atalaya server: database does not support manual checks")
			return exitRuntime
		}
		settingsRepository, settingsOK := owner.(interface {
			GetCheckSettings(context.Context) (checks.Settings, error)
		})
		policy := checks.NetworkPolicy{}
		if settingsOK {
			current, settingsErr := settingsRepository.GetCheckSettings(ctx)
			if settingsErr != nil {
				_ = owner.Close()
				fmt.Fprintf(stderr, "atalaya server: read check settings: %v\n", settingsErr)
				return exitRuntime
			}
			policy, err = checks.NewNetworkPolicy(current.AllowedPrivateCIDRs)
			if err != nil {
				_ = owner.Close()
				fmt.Fprintf(stderr, "atalaya server: validate check settings: %v\n", err)
				return exitRuntime
			}
		}
		probe := checks.NewProbe(policy)
		if settingsOK {
			probe.PolicyProvider = func(runContext context.Context) (checks.NetworkPolicy, error) {
				current, settingsErr := settingsRepository.GetCheckSettings(runContext)
				if settingsErr != nil {
					return checks.NetworkPolicy{}, settingsErr
				}
				return checks.NewNetworkPolicy(current.AllowedPrivateCIDRs)
			}
		}
		jobs = append(jobs, func(runContext context.Context) error {
			return application.runChecks(runContext, schedulerRepository, manualRepository, probe)
		})
	}
	if repository, ok := owner.(notifications.Repository); ok && application.runNotifications != nil {
		jobs = append(jobs, func(runContext context.Context) error { return application.runNotifications(runContext, repository) })
	}
	var runErr error
	if len(jobs) > 0 {
		runErr = runServerAndJobs(ctx, application.runServer, processConfig, handler, jobs...)
	} else {
		runErr = application.runServer(ctx, processConfig, handler)
	}
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

func runServerAndJobs(ctx context.Context, runServer func(context.Context, server.Config, http.Handler) error, config server.Config, handler http.Handler, jobs ...func(context.Context) error) error {
	coordinated, cancel := context.WithCancel(ctx)
	defer cancel()
	errorsChannel := make(chan error, len(jobs)+1)
	go func() { errorsChannel <- runServer(coordinated, config, handler) }()
	for _, job := range jobs {
		go func(run func(context.Context) error) { errorsChannel <- run(coordinated) }(job)
	}
	first := <-errorsChannel
	cancel()
	firstErr := first
	if firstErr != nil && !errors.Is(firstErr, context.Canceled) {
		cancel()
	}
	for range jobs {
		err := <-errorsChannel
		if err != nil && !errors.Is(err, context.Canceled) && firstErr == nil {
			firstErr = err
			cancel()
		}
	}
	if firstErr != nil && !errors.Is(firstErr, context.Canceled) {
		return firstErr
	}
	return nil
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

func containsHelpFlag(args []string) bool {
	for _, argument := range args {
		if argument == "-h" || argument == "--help" {
			return true
		}
	}
	return false
}
