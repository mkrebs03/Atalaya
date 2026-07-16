// Package config loads and validates Atalaya startup configuration.
package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Mode controls security-sensitive startup behavior.
type Mode string

const (
	ModeProduction  Mode = "production"
	ModeDevelopment Mode = "development"
)

// ServerConfig is the complete Phase 1A server startup configuration.
type ServerConfig struct {
	Listen        string `yaml:"listen"`
	Mode          Mode   `yaml:"mode"`
	PublicURL     string `yaml:"public_url"`
	DatabasePath  string `yaml:"database_path"`
	MasterKeyPath string `yaml:"master_key_path"`
}

// LoadServer reads a strict, single-document Phase 1A server configuration.
func LoadServer(path string) (ServerConfig, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return ServerConfig{}, fmt.Errorf("resolve server configuration path: %w", err)
	}

	contents, err := os.ReadFile(absolutePath)
	if err != nil {
		return ServerConfig{}, fmt.Errorf("read server configuration: %w", err)
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
	decoder.KnownFields(true)

	var loaded ServerConfig
	if err := decoder.Decode(&loaded); err != nil {
		return ServerConfig{}, fmt.Errorf("decode server configuration: %w", err)
	}

	var additional any
	if err := decoder.Decode(&additional); !errors.Is(err, io.EOF) {
		if err != nil {
			return ServerConfig{}, fmt.Errorf("decode server configuration: %w", err)
		}
		return ServerConfig{}, fmt.Errorf("server configuration must contain exactly one YAML document")
	}

	if err := loaded.Validate(); err != nil {
		return ServerConfig{}, err
	}

	baseDirectory := filepath.Dir(absolutePath)
	loaded.DatabasePath = resolvePath(baseDirectory, loaded.DatabasePath)
	loaded.MasterKeyPath = resolvePath(baseDirectory, loaded.MasterKeyPath)
	return loaded, nil
}

// Validate rejects incomplete or insecure server startup configuration.
func (serverConfig ServerConfig) Validate() error {
	if serverConfig.DatabasePath == "" {
		return fmt.Errorf("database path is required")
	}
	if serverConfig.MasterKeyPath == "" {
		return fmt.Errorf("master key path is required")
	}

	return ValidateServerSettings(serverConfig.Listen, serverConfig.Mode, serverConfig.PublicURL)
}

// ValidateServerSettings validates listener, mode, and browser origin together.
func ValidateServerSettings(listen string, mode Mode, publicURL string) error {
	if _, err := validateListener(listen); err != nil {
		return err
	}

	parsedPublicURL, err := validatePublicURL(publicURL)
	if err != nil {
		return err
	}

	switch mode {
	case ModeProduction:
		if parsedPublicURL.Scheme != "https" {
			return fmt.Errorf("production mode requires an HTTPS public URL")
		}
	case ModeDevelopment:
		if parsedPublicURL.Scheme == "http" && !isLoopbackHost(parsedPublicURL.Hostname()) {
			return fmt.Errorf("development HTTP requires a loopback public URL")
		}
	default:
		return fmt.Errorf("unsupported mode %q", mode)
	}

	return nil
}

func resolvePath(baseDirectory, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(baseDirectory, path)
}

func validateListener(address string) (string, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid listen address")
	}
	if host == "" {
		return "", fmt.Errorf("listen address requires an explicit host")
	}
	if !isLoopbackHost(host) {
		return "", fmt.Errorf("listen address must use a loopback host")
	}

	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("listen address requires a port between 1 and 65535")
	}

	return host, nil
}

func validatePublicURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, fmt.Errorf("public URL is required")
	}
	if strings.ContainsAny(raw, "?#") {
		return nil, fmt.Errorf("public URL must be an origin without path, query, or fragment")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid public URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("public URL must use HTTP or HTTPS")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("public URL requires a host")
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return nil, fmt.Errorf("public URL must not include an empty port")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("public URL must not include user information")
	}
	if parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.ForceQuery || parsed.Opaque != "" {
		return nil, fmt.Errorf("public URL must be an origin without path, query, or fragment")
	}
	if err := validateURLPort(parsed); err != nil {
		return nil, err
	}

	return parsed, nil
}

func validateURLPort(parsed *url.URL) error {
	portText := parsed.Port()
	if portText == "" {
		return nil
	}

	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("public URL requires a port between 1 and 65535")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
