package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadServerLoadsStrictConfigAndResolvesRelativePaths(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	configPath := filepath.Join(directory, "config", "server.yaml")
	writeConfig(t, configPath, `listen: 127.0.0.1:8080
mode: production
public_url: https://atalaya.example.test
database_path: ../data/atalaya.db
master_key_path: master.key
`)

	loaded, err := LoadServer(configPath)
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}

	if loaded.Listen != "127.0.0.1:8080" || loaded.Mode != ModeProduction || loaded.PublicURL != "https://atalaya.example.test" {
		t.Fatalf("LoadServer() settings = %#v", loaded)
	}
	if loaded.DatabasePath != filepath.Join(directory, "data", "atalaya.db") {
		t.Errorf("DatabasePath = %q", loaded.DatabasePath)
	}
	if loaded.MasterKeyPath != filepath.Join(directory, "config", "master.key") {
		t.Errorf("MasterKeyPath = %q", loaded.MasterKeyPath)
	}
}

func TestTrustedProxyDefaultsAndValidation(t *testing.T) {
	base := ServerConfig{Listen: "127.0.0.1:8080", Mode: ModeProduction, PublicURL: "https://atalaya.example", DatabasePath: "db", MasterKeyPath: "key"}
	if err := base.Validate(); err != nil {
		t.Fatalf("default none: %v", err)
	}
	base.TrustedProxy = TrustedProxyTailscaleServe
	if err := base.Validate(); err != nil {
		t.Fatalf("tailscale serve: %v", err)
	}
	base.Listen = "0.0.0.0:8080"
	if err := base.Validate(); err == nil {
		t.Fatal("non-loopback tailscale_serve listener accepted")
	}
	base.Listen = "127.0.0.1:8080"
	base.Mode = ModeDevelopment
	if err := base.Validate(); err == nil {
		t.Fatal("development tailscale_serve accepted")
	}
	base.Mode = ModeProduction
	base.TrustedProxy = "unknown"
	if err := base.Validate(); err == nil {
		t.Fatal("unknown trusted proxy accepted")
	}
}

func TestLoadServerPreservesAbsolutePaths(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	configPath := filepath.Join(directory, "server.yaml")
	writeConfig(t, configPath, `listen: localhost:8080
mode: development
public_url: http://localhost:5173
database_path: /var/lib/atalaya/../atalaya/atalaya.db
master_key_path: /etc/atalaya/master.key
`)

	loaded, err := LoadServer(configPath)
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}
	if loaded.DatabasePath != "/var/lib/atalaya/atalaya.db" {
		t.Errorf("DatabasePath = %q", loaded.DatabasePath)
	}
	if loaded.MasterKeyPath != "/etc/atalaya/master.key" {
		t.Errorf("MasterKeyPath = %q", loaded.MasterKeyPath)
	}
}

func TestLoadServerRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	valid := `listen: 127.0.0.1:8080
mode: production
public_url: https://atalaya.example.test
database_path: atalaya.db
master_key_path: master.key
`
	tests := []struct {
		name    string
		content string
		match   string
	}{
		{name: "unknown field", content: valid + "log_level: debug\n", match: "field log_level not found"},
		{name: "additional document", content: valid + "---\nlisten: 127.0.0.1:9999\n", match: "exactly one"},
		{name: "empty document", content: "", match: "decode server configuration"},
		{name: "missing database", content: strings.ReplaceAll(valid, "database_path: atalaya.db\n", ""), match: "database path is required"},
		{name: "missing key", content: strings.ReplaceAll(valid, "master_key_path: master.key\n", ""), match: "master key path is required"},
		{name: "missing listen", content: strings.ReplaceAll(valid, "listen: 127.0.0.1:8080\n", ""), match: "invalid listen"},
		{name: "unknown mode", content: strings.ReplaceAll(valid, "mode: production", "mode: staging"), match: "unsupported mode"},
		{name: "public listener", content: strings.ReplaceAll(valid, "127.0.0.1:8080", "0.0.0.0:8080"), match: "loopback"},
		{name: "production HTTP", content: strings.ReplaceAll(valid, "https://", "http://"), match: "HTTPS"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			configPath := filepath.Join(t.TempDir(), "server.yaml")
			writeConfig(t, configPath, test.content)

			_, err := LoadServer(configPath)
			if err == nil || !strings.Contains(err.Error(), test.match) {
				t.Fatalf("LoadServer() error = %v, want containing %q", err, test.match)
			}
			if !errors.Is(err, ErrInvalidServerConfig) {
				t.Fatalf("LoadServer() error = %v, want ErrInvalidServerConfig", err)
			}
		})
	}
}

func TestLoadServerKeepsFilesystemFailureOperational(t *testing.T) {
	t.Parallel()

	_, err := LoadServer(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("LoadServer() error = nil")
	}
	if errors.Is(err, ErrInvalidServerConfig) {
		t.Fatalf("LoadServer() mislabeled filesystem failure: %v", err)
	}
}

func TestLoadServerDoesNotExposeMalformedPublicURL(t *testing.T) {
	t.Parallel()

	secret := "configuration-secret"
	configPath := filepath.Join(t.TempDir(), "server.yaml")
	writeConfig(t, configPath, `listen: 127.0.0.1:8080
mode: production
public_url: https://example.test:`+secret+`
database_path: atalaya.db
master_key_path: master.key
`)

	_, err := LoadServer(configPath)
	if err == nil {
		t.Fatal("LoadServer() error = nil")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("LoadServer() exposed public URL content: %q", err)
	}
}

func TestValidateServerSettingsAcceptsDocumentedCombinations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		listen    string
		mode      Mode
		publicURL string
	}{
		{listen: "127.0.0.1:8080", mode: ModeProduction, publicURL: "https://atalaya.example.test"},
		{listen: "localhost:8080", mode: ModeDevelopment, publicURL: "http://localhost:5173"},
		{listen: "[::1]:8080", mode: ModeDevelopment, publicURL: "http://[::1]:5173"},
		{listen: "127.0.0.1:8080", mode: ModeDevelopment, publicURL: "https://atalaya.tailnet.example"},
	}

	for _, test := range tests {
		if err := ValidateServerSettings(test.listen, test.mode, test.publicURL); err != nil {
			t.Errorf("ValidateServerSettings(%q, %q) error = %v", test.listen, test.mode, err)
		}
	}
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
