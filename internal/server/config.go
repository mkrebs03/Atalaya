// Package server validates startup configuration and runs the HTTP process.
package server

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Mode controls security-sensitive startup behavior.
type Mode string

const (
	ModeProduction  Mode = "production"
	ModeDevelopment Mode = "development"
)

// Config contains the Phase 0 server startup configuration.
type Config struct {
	Listen          string
	Mode            Mode
	PublicURL       string
	ShutdownTimeout time.Duration
}

// Validate rejects configurations that could weaken production transport security.
func (config Config) Validate() error {
	_, err := validateListener(config.Listen)
	if err != nil {
		return err
	}

	publicURL, err := validatePublicURL(config.PublicURL)
	if err != nil {
		return err
	}

	switch config.Mode {
	case ModeProduction:
		if publicURL.Scheme != "https" {
			return fmt.Errorf("production mode requires an HTTPS public URL")
		}
	case ModeDevelopment:
		if publicURL.Scheme == "http" && !isLoopbackHost(publicURL.Hostname()) {
			return fmt.Errorf("development HTTP requires a loopback public URL")
		}
	default:
		return fmt.Errorf("unsupported mode %q", config.Mode)
	}

	if config.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive")
	}

	return nil
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
