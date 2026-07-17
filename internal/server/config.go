// Package server validates startup configuration and runs the HTTP process.
package server

import (
	"fmt"
	"time"

	startupconfig "github.com/mkrebs03/atalaya/internal/config"
)

// Mode controls security-sensitive startup behavior.
type Mode = startupconfig.Mode

const (
	ModeProduction  = startupconfig.ModeProduction
	ModeDevelopment = startupconfig.ModeDevelopment
)

// Config contains the validated HTTP process settings derived from Phase 1A YAML.
type Config struct {
	Listen          string
	Mode            Mode
	PublicURL       string
	ShutdownTimeout time.Duration
}

// Validate rejects configurations that could weaken production transport security.
func (config Config) Validate() error {
	if err := startupconfig.ValidateServerSettings(config.Listen, config.Mode, config.PublicURL); err != nil {
		return err
	}

	if config.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive")
	}

	return nil
}
