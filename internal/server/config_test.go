package server

import (
	"strings"
	"testing"
	"time"
)

func TestConfigValidateAcceptsSecureCombinations(t *testing.T) {
	t.Parallel()

	tests := []Config{
		{
			Listen:          "127.0.0.1:8080",
			Mode:            ModeProduction,
			PublicURL:       "https://atalaya.example.test",
			ShutdownTimeout: 10 * time.Second,
		},
		{
			Listen:          "localhost:8080",
			Mode:            ModeDevelopment,
			PublicURL:       "http://localhost:5173",
			ShutdownTimeout: time.Second,
		},
		{
			Listen:          "[::1]:8080",
			Mode:            ModeDevelopment,
			PublicURL:       "http://[::1]:5173",
			ShutdownTimeout: time.Second,
		},
		{
			Listen:          "127.0.0.1:8080",
			Mode:            ModeDevelopment,
			PublicURL:       "https://atalaya.tailnet.example",
			ShutdownTimeout: time.Second,
		},
	}

	for _, config := range tests {
		config := config
		t.Run(config.Listen+config.PublicURL, func(t *testing.T) {
			t.Parallel()
			if err := config.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestConfigValidateRejectsInvalidOrInsecureCombinations(t *testing.T) {
	t.Parallel()

	valid := Config{
		Listen:          "127.0.0.1:8080",
		Mode:            ModeProduction,
		PublicURL:       "https://atalaya.example.test",
		ShutdownTimeout: time.Second,
	}
	tests := []struct {
		name   string
		change func(*Config)
		match  string
	}{
		{name: "missing public URL", change: func(c *Config) { c.PublicURL = "" }, match: "required"},
		{name: "production HTTP", change: func(c *Config) { c.PublicURL = "http://localhost" }, match: "HTTPS"},
		{name: "development public host", change: func(c *Config) { c.Mode = ModeDevelopment; c.PublicURL = "http://dev.example.test" }, match: "loopback"},
		{name: "development public listener", change: func(c *Config) {
			c.Mode = ModeDevelopment
			c.Listen = "0.0.0.0:8080"
			c.PublicURL = "http://localhost:8080"
		}, match: "loopback"},
		{name: "production wildcard listener", change: func(c *Config) { c.Listen = "0.0.0.0:8080" }, match: "loopback"},
		{name: "production external listener", change: func(c *Config) { c.Listen = "atalaya.example.test:8080" }, match: "loopback"},
		{name: "development HTTPS external listener", change: func(c *Config) {
			c.Mode = ModeDevelopment
			c.Listen = "192.0.2.10:8080"
		}, match: "loopback"},
		{name: "unknown mode", change: func(c *Config) { c.Mode = "staging" }, match: "unsupported mode"},
		{name: "missing listener host", change: func(c *Config) { c.Listen = ":8080" }, match: "explicit host"},
		{name: "missing listener port", change: func(c *Config) { c.Listen = "localhost" }, match: "invalid listen"},
		{name: "zero listener port", change: func(c *Config) { c.Listen = "localhost:0" }, match: "between 1 and 65535"},
		{name: "invalid scheme", change: func(c *Config) { c.PublicURL = "ftp://example.test" }, match: "HTTP or HTTPS"},
		{name: "userinfo", change: func(c *Config) { c.PublicURL = "https://user@example.test" }, match: "user information"},
		{name: "path", change: func(c *Config) { c.PublicURL = "https://example.test/app" }, match: "origin"},
		{name: "root path", change: func(c *Config) { c.PublicURL = "https://example.test/" }, match: "origin"},
		{name: "query", change: func(c *Config) { c.PublicURL = "https://example.test?mode=1" }, match: "origin"},
		{name: "empty query", change: func(c *Config) { c.PublicURL = "https://example.test?" }, match: "origin"},
		{name: "fragment", change: func(c *Config) { c.PublicURL = "https://example.test#fragment" }, match: "origin"},
		{name: "empty fragment", change: func(c *Config) { c.PublicURL = "https://example.test#" }, match: "origin"},
		{name: "empty port", change: func(c *Config) { c.PublicURL = "https://example.test:" }, match: "empty port"},
		{name: "zero shutdown timeout", change: func(c *Config) { c.ShutdownTimeout = 0 }, match: "shutdown timeout"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := valid
			test.change(&config)
			err := config.Validate()
			if err == nil || !strings.Contains(err.Error(), test.match) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.match)
			}
		})
	}
}

func TestConfigValidationDoesNotExposeMalformedPublicURL(t *testing.T) {
	t.Parallel()

	secret := "phase-zero-secret"
	config := Config{
		Listen:          "127.0.0.1:8080",
		Mode:            ModeProduction,
		PublicURL:       "https://example.test:" + secret,
		ShutdownTimeout: time.Second,
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want malformed URL rejection")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Validate() error exposed public URL content: %q", err)
	}
}
