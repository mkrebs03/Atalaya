package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/auth"
	startupconfig "github.com/mkrebs03/atalaya/internal/config"
	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
	"github.com/mkrebs03/atalaya/internal/database"
	"github.com/mkrebs03/atalaya/internal/server"
)

const cliTestPasswordPHC = "$argon2id$v=19$m=65536,t=3,p=4$AAECAwQFBgcICQoLDA0ODw$hTsnKkTbFCHAKWJmmlXrCZTzyrOF7RxMeSU+7hm6tJ4"

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
		{name: "root help", args: []string{"--help"}, wantCode: exitSuccess, wantOutput: "migrate"},
		{name: "help command", args: []string{"help"}, wantCode: exitSuccess, wantOutput: "admin"},
		{name: "help server", args: []string{"help", "server"}, wantCode: exitSuccess, wantOutput: "--config"},
		{name: "help migrate", args: []string{"help", "migrate"}, wantCode: exitSuccess, wantOutput: "atalaya migrate"},
		{name: "help admin", args: []string{"help", "admin"}, wantCode: exitSuccess, wantOutput: "create"},
		{name: "help unknown", args: []string{"help", "unknown"}, wantCode: exitUsage, wantError: "unknown command"},
		{name: "version", args: []string{"version"}, wantCode: exitSuccess, wantOutput: "atalaya dev\n"},
		{name: "version help", args: []string{"version", "--help"}, wantCode: exitSuccess, wantOutput: "Usage: atalaya version"},
		{name: "version argument", args: []string{"version", "extra"}, wantCode: exitUsage, wantError: "accepts no arguments"},
		{name: "server", args: []string{"server"}, wantCode: exitSuccess},
		{name: "server help", args: []string{"server", "--help"}, wantCode: exitSuccess, wantOutput: "--config"},
		{name: "migrate", args: []string{"migrate"}, wantCode: exitSuccess, wantOutput: "migrations applied"},
		{name: "migrate help", args: []string{"migrate", "--help"}, wantCode: exitSuccess, wantOutput: "--config"},
		{name: "admin missing subcommand", args: []string{"admin"}, wantCode: exitUsage, wantError: "Usage: atalaya admin"},
		{name: "admin unknown", args: []string{"admin", "unknown"}, wantCode: exitUsage, wantError: "unknown command"},
		{name: "admin help", args: []string{"admin", "--help"}, wantCode: exitSuccess, wantOutput: "create"},
		{name: "admin create help", args: []string{"admin", "create", "--help"}, wantCode: exitSuccess, wantOutput: "--username"},
		{name: "admin create", args: []string{"admin", "create"}, wantCode: exitSuccess, wantOutput: "administrator \"admin\" created"},
		{name: "agent reserved", args: []string{"agent"}, wantCode: exitRuntime, wantError: "not implemented"},
		{name: "backup reserved", args: []string{"backup"}, wantCode: exitRuntime, wantError: "not implemented"},
		{name: "verify reserved", args: []string{"verify"}, wantCode: exitRuntime, wantError: "not implemented"},
		{name: "reserved help", args: []string{"agent", "--help"}, wantCode: exitSuccess, wantOutput: "later phase"},
		{name: "reserved invalid argument", args: []string{"backup", "--output", "file"}, wantCode: exitUsage, wantError: "unexpected arguments"},
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

func TestServerBootstrapsDatabaseBeforeHandlerAndListener(t *testing.T) {
	t.Parallel()

	application := testApplication()
	events := make([]string, 0, 6)
	key := bytes.Repeat([]byte{0x42}, 32)
	owner := &fakeDatabase{events: &events}
	application.loadConfig = func(path string) (startupconfig.ServerConfig, error) {
		events = append(events, "config:"+path)
		return testServerConfig(), nil
	}
	application.loadMasterKey = func(string) ([]byte, error) {
		events = append(events, "key")
		return key, nil
	}
	application.openDatabase = func(context.Context, string, []byte) (databaseHandle, error) {
		events = append(events, "open")
		return owner, nil
	}
	application.newHandler = func() (http.Handler, error) {
		events = append(events, "handler")
		return http.NotFoundHandler(), nil
	}
	application.runServer = func(_ context.Context, config server.Config, _ http.Handler) error {
		events = append(events, "listen")
		if config.Listen != "127.0.0.1:8080" || config.Mode != server.ModeDevelopment {
			t.Fatalf("server config = %#v", config)
		}
		return nil
	}

	var stderr bytes.Buffer
	if code := application.Run([]string{"server", "--config", "/tmp/server.yaml"}, io.Discard, &stderr); code != exitSuccess {
		t.Fatalf("Run() = %d, stderr=%q", code, stderr.String())
	}
	want := []string{"config:/tmp/server.yaml", "key", "open", "handler", "listen", "close"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	if !allZero(key) {
		t.Fatal("server bootstrap key was not cleared")
	}
}

func TestServerBootstrapFailuresNeverReachListener(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		prepare  func(*Application, *fakeDatabase)
		wantCode int
	}{
		{name: "invalid config", wantCode: exitUsage, prepare: func(application *Application, _ *fakeDatabase) {
			application.loadConfig = func(string) (startupconfig.ServerConfig, error) {
				return startupconfig.ServerConfig{}, fmt.Errorf("%w: bad YAML", startupconfig.ErrInvalidServerConfig)
			}
		}},
		{name: "missing config", wantCode: exitRuntime, prepare: func(application *Application, _ *fakeDatabase) {
			application.loadConfig = func(string) (startupconfig.ServerConfig, error) {
				return startupconfig.ServerConfig{}, errors.New("config unavailable")
			}
		}},
		{name: "key", wantCode: exitRuntime, prepare: func(application *Application, _ *fakeDatabase) {
			application.loadMasterKey = func(string) ([]byte, error) { return nil, errors.New("key unavailable") }
		}},
		{name: "database", wantCode: exitRuntime, prepare: func(application *Application, _ *fakeDatabase) {
			application.openDatabase = func(context.Context, string, []byte) (databaseHandle, error) {
				return nil, errors.New("wrong key")
			}
		}},
		{name: "handler", wantCode: exitRuntime, prepare: func(application *Application, _ *fakeDatabase) {
			application.newHandler = func() (http.Handler, error) { return nil, errors.New("assets unavailable") }
		}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			application := testApplication()
			owner := &fakeDatabase{}
			application.openDatabase = func(context.Context, string, []byte) (databaseHandle, error) { return owner, nil }
			test.prepare(&application, owner)
			application.runServer = func(context.Context, server.Config, http.Handler) error {
				t.Fatal("listener reached after bootstrap failure")
				return nil
			}
			var stderr bytes.Buffer
			if code := application.Run([]string{"server"}, io.Discard, &stderr); code != test.wantCode {
				t.Fatalf("Run() = %d, want %d; stderr=%q", code, test.wantCode, stderr.String())
			}
			if test.name == "handler" && !owner.closed {
				t.Fatal("database was not closed after handler failure")
			}
		})
	}
}

func TestCommandsRejectLegacyFlagsPositionalsAndEmptyConfig(t *testing.T) {
	t.Parallel()

	commands := [][]string{
		{"server", "--listen", "127.0.0.1:8080"},
		{"server", "extra"},
		{"server", "--config", ""},
		{"migrate", "extra"},
		{"migrate", "--config", ""},
		{"admin", "create", "extra"},
		{"admin", "create", "--config", ""},
		{"admin", "create", "--username", "invalid user"},
	}
	for _, args := range commands {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			application := testApplication()
			application.loadConfig = func(string) (startupconfig.ServerConfig, error) {
				t.Fatal("config loaded for invalid arguments")
				return startupconfig.ServerConfig{}, nil
			}
			if code := application.Run(args, io.Discard, io.Discard); code != exitUsage {
				t.Fatalf("Run() = %d, want %d", code, exitUsage)
			}
		})
	}
}

func TestMigrateUsesSharedBootstrapWithoutHTTP(t *testing.T) {
	t.Parallel()

	application := testApplication()
	events := make([]string, 0, 4)
	owner := &fakeDatabase{events: &events}
	application.loadConfig = func(path string) (startupconfig.ServerConfig, error) {
		events = append(events, "config:"+path)
		return testServerConfig(), nil
	}
	application.loadMasterKey = func(string) ([]byte, error) {
		events = append(events, "key")
		return make([]byte, 32), nil
	}
	application.openDatabase = func(context.Context, string, []byte) (databaseHandle, error) {
		events = append(events, "open")
		return owner, nil
	}
	application.newHandler = func() (http.Handler, error) {
		t.Fatal("migrate constructed HTTP handler")
		return nil, nil
	}
	application.runServer = func(context.Context, server.Config, http.Handler) error {
		t.Fatal("migrate started HTTP")
		return nil
	}

	if code := application.Run([]string{"migrate", "--config", "/tmp/migrate.yaml"}, io.Discard, io.Discard); code != exitSuccess {
		t.Fatalf("Run() = %d", code)
	}
	want := []string{"config:/tmp/migrate.yaml", "key", "open", "close"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestAdminCreatePreflightsPromptsHashesThenWrites(t *testing.T) {
	t.Parallel()

	application := testApplication()
	events := make([]string, 0, 10)
	owner := &fakeDatabase{events: &events}
	key := bytes.Repeat([]byte{0x33}, 32)
	application.loadConfig = func(path string) (startupconfig.ServerConfig, error) {
		events = append(events, "config:"+path)
		return testServerConfig(), nil
	}
	application.loadMasterKey = func(string) ([]byte, error) {
		events = append(events, "key")
		return key, nil
	}
	application.preflightDatabase = func(context.Context, string, []byte) (database.PreflightResult, error) {
		events = append(events, "preflight")
		return database.PreflightResult{State: database.StateFresh}, nil
	}
	reads := 0
	application.readPassword = func(string, io.Writer) ([]byte, error) {
		reads++
		events = append(events, fmt.Sprintf("read%d", reads))
		return []byte("correct horse battery staple"), nil
	}
	application.hashPassword = func([]byte) (string, error) {
		events = append(events, "hash")
		return cliTestPasswordPHC, nil
	}
	application.openDatabase = func(context.Context, string, []byte) (databaseHandle, error) {
		events = append(events, "open")
		return owner, nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := application.Run([]string{
		"admin", "create", "--config", "/tmp/admin.yaml", "--username", " Admin ",
	}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("Run() = %d, stderr=%q", code, stderr.String())
	}
	want := []string{
		"config:/tmp/admin.yaml", "key", "preflight", "read1", "read2", "hash", "open", "create", "close",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	if owner.administrator.Username != "admin" || owner.administrator.PasswordHash != cliTestPasswordPHC {
		t.Fatalf("administrator = %#v", owner.administrator)
	}
	if !strings.Contains(stdout.String(), `administrator "admin" created`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !allZero(key) {
		t.Fatal("admin bootstrap key was not cleared")
	}
}

func TestAdminCreateRejectsExistingAdministratorBeforePrompt(t *testing.T) {
	t.Parallel()

	application := testApplication()
	application.preflightDatabase = func(context.Context, string, []byte) (database.PreflightResult, error) {
		return database.PreflightResult{State: database.StateAtalaya, HasActiveAdministrator: true}, nil
	}
	application.readPassword = func(string, io.Writer) ([]byte, error) {
		t.Fatal("password prompted for existing administrator")
		return nil, nil
	}
	application.openDatabase = func(context.Context, string, []byte) (databaseHandle, error) {
		t.Fatal("writable database opened for existing administrator")
		return nil, nil
	}

	var stderr bytes.Buffer
	if code := application.Run([]string{"admin", "create"}, io.Discard, &stderr); code != exitRuntime {
		t.Fatalf("Run() = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), database.ErrAdministratorExists.Error()) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestAdminPasswordMismatchDoesNotCreateFreshDatabase(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	databasePath := filepath.Join(directory, "atalaya.db")
	keyPath := filepath.Join(directory, "master.key")
	configPath := filepath.Join(directory, "server.yaml")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x41}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration := fmt.Sprintf(`listen: 127.0.0.1:8080
mode: development
public_url: http://127.0.0.1:8080
database_path: %s
master_key_path: %s
`, databasePath, keyPath)
	if err := os.WriteFile(configPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}

	application := testApplication()
	application.loadConfig = startupconfig.LoadServer
	application.loadMasterKey = atalayacrypto.LoadMasterKey
	application.preflightDatabase = database.Preflight
	application.openDatabase = func(ctx context.Context, path string, key []byte) (databaseHandle, error) {
		return database.Open(ctx, path, key)
	}
	values := [][]byte{[]byte("correct horse battery staple"), []byte("different horse battery staple")}
	index := 0
	application.readPassword = func(string, io.Writer) ([]byte, error) {
		value := values[index]
		index++
		return value, nil
	}

	if code := application.Run([]string{"admin", "create", "--config", configPath}, io.Discard, io.Discard); code != exitRuntime {
		t.Fatalf("Run() = %d", code)
	}
	if _, err := os.Stat(databasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database path exists or stat failed: %v", err)
	}
	for _, value := range values {
		if !allZero(value) {
			t.Fatal("password buffer was not cleared")
		}
	}
}

func TestDefaultConfigPathIsSharedByPhase1ACommands(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"server"}, {"migrate"}, {"admin", "create"}} {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			application := testApplication()
			application.loadConfig = func(path string) (startupconfig.ServerConfig, error) {
				if path != defaultServerConfigPath {
					t.Fatalf("config path = %q", path)
				}
				return testServerConfig(), nil
			}
			if code := application.Run(args, io.Discard, io.Discard); code != exitSuccess {
				t.Fatalf("Run() = %d", code)
			}
		})
	}
}

type fakeDatabase struct {
	events        *[]string
	administrator auth.Administrator
	createErr     error
	closeErr      error
	closed        bool
	resetHash     string
}

func (database *fakeDatabase) FindActiveAdministrator(context.Context, string) (auth.Administrator, error) {
	return database.administrator, nil
}
func (*fakeDatabase) CreateSessionIfAdministratorVersion(context.Context, auth.Session, int) error {
	return nil
}
func (*fakeDatabase) ValidateAndTouchSession(context.Context, []byte, time.Time, time.Time) (auth.Administrator, auth.Session, error) {
	return auth.Administrator{}, auth.Session{}, auth.ErrAuthenticationRequired
}
func (*fakeDatabase) RevokeSession(context.Context, []byte, time.Time) error { return nil }
func (*fakeDatabase) ChangePasswordAndReplaceSessions(context.Context, string, int, string, time.Time, auth.Session) error {
	return nil
}
func (database *fakeDatabase) ResetPasswordAndRevokeSessions(_ context.Context, hash string, _ time.Time) error {
	database.resetHash = hash
	return nil
}

func TestAdminResetPasswordPreflightsThenRevokesSessions(t *testing.T) {
	application := testApplication()
	owner := &fakeDatabase{}
	application.preflightDatabase = func(context.Context, string, []byte) (database.PreflightResult, error) {
		return database.PreflightResult{State: database.StateAtalaya, SchemaVersion: 2, HasActiveAdministrator: true}, nil
	}
	application.openDatabase = func(context.Context, string, []byte) (databaseHandle, error) { return owner, nil }
	var stdout, stderr bytes.Buffer
	if code := application.Run([]string{"admin", "reset-password"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if owner.resetHash != cliTestPasswordPHC || !strings.Contains(stdout.String(), "all sessions revoked") {
		t.Fatalf("reset = %q, stdout=%q", owner.resetHash, stdout.String())
	}
}

func TestAdminResetPasswordDoesNotPromptWithoutAdministrator(t *testing.T) {
	application := testApplication()
	prompted := false
	application.readPassword = func(string, io.Writer) ([]byte, error) { prompted = true; return nil, nil }
	var stdout, stderr bytes.Buffer
	if code := application.Run([]string{"admin", "reset-password"}, &stdout, &stderr); code != exitRuntime {
		t.Fatalf("code=%d", code)
	}
	if prompted {
		t.Fatal("password prompt ran before active-administrator preflight")
	}
}

func (database *fakeDatabase) CreateAdministrator(_ context.Context, administrator auth.Administrator) error {
	if database.events != nil {
		*database.events = append(*database.events, "create")
	}
	database.administrator = administrator
	return database.createErr
}

func (database *fakeDatabase) Close() error {
	if database.events != nil {
		*database.events = append(*database.events, "close")
	}
	database.closed = true
	return database.closeErr
}

func testApplication() Application {
	return Application{
		version: "dev",
		newHandler: func() (http.Handler, error) {
			return http.NotFoundHandler(), nil
		},
		runServer: func(context.Context, server.Config, http.Handler) error {
			return nil
		},
		loadConfig: func(string) (startupconfig.ServerConfig, error) {
			return testServerConfig(), nil
		},
		loadMasterKey: func(string) ([]byte, error) {
			return make([]byte, 32), nil
		},
		preflightDatabase: func(context.Context, string, []byte) (database.PreflightResult, error) {
			return database.PreflightResult{State: database.StateFresh}, nil
		},
		openDatabase: func(context.Context, string, []byte) (databaseHandle, error) {
			return &fakeDatabase{}, nil
		},
		readPassword: func(string, io.Writer) ([]byte, error) {
			return []byte("correct horse battery staple"), nil
		},
		hashPassword: func([]byte) (string, error) {
			return cliTestPasswordPHC, nil
		},
	}
}

func testServerConfig() startupconfig.ServerConfig {
	return startupconfig.ServerConfig{
		Listen:        "127.0.0.1:8080",
		Mode:          startupconfig.ModeDevelopment,
		PublicURL:     "http://127.0.0.1:8080",
		DatabasePath:  "/tmp/atalaya.db",
		MasterKeyPath: "/tmp/master.key",
	}
}
