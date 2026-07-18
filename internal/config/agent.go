package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/unix"
)

var ErrInvalidAgentConfig = errors.New("invalid agent configuration")

type AgentConfig struct {
	Listen         string `yaml:"listen"`
	ServerURL      string `yaml:"server_url"`
	AdvertisedURL  string `yaml:"advertised_url"`
	CredentialPath string `yaml:"credential_path"`
}

func LoadAgent(path string) (AgentConfig, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return AgentConfig{}, fmt.Errorf("resolve agent configuration path: %w", err)
	}
	b, err := readAgentConfig(abs)
	if err != nil {
		return AgentConfig{}, err
	}
	d := yaml.NewDecoder(strings.NewReader(string(b)))
	d.KnownFields(true)
	var c AgentConfig
	if err := d.Decode(&c); err != nil {
		return AgentConfig{}, invalidAgent("decode agent configuration", err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return AgentConfig{}, invalidAgent("decode agent configuration", err)
		}
		return AgentConfig{}, invalidAgent("agent configuration must contain exactly one YAML document", nil)
	}
	if err := c.Validate(); err != nil {
		return AgentConfig{}, invalidAgent("validate agent configuration", err)
	}
	if !filepath.IsAbs(c.CredentialPath) {
		c.CredentialPath = filepath.Join(filepath.Dir(abs), c.CredentialPath)
	}
	c.CredentialPath = filepath.Clean(c.CredentialPath)
	return c, nil
}

func (c AgentConfig) Validate() error {
	if _, err := validateListener(c.Listen); err != nil {
		return err
	}
	if err := validateOrigin(c.ServerURL); err != nil {
		return fmt.Errorf("server URL: %w", err)
	}
	if err := validateAgentURL(c.AdvertisedURL); err != nil {
		return fmt.Errorf("advertised URL: %w", err)
	}
	if strings.TrimSpace(c.CredentialPath) == "" {
		return fmt.Errorf("credential path is required")
	}
	return nil
}

func invalidAgent(message string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: %s", ErrInvalidAgentConfig, message)
	}
	return fmt.Errorf("%w: %s: %v", ErrInvalidAgentConfig, message, cause)
}

func validateOrigin(raw string) error {
	if raw == "" || strings.ContainsAny(raw, "?#") {
		return fmt.Errorf("must be an HTTP(S) origin")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawPath != "" || u.Opaque != "" || strings.HasSuffix(u.Host, ":") {
		return fmt.Errorf("must be an HTTP(S) origin")
	}
	return validateURLPort(u)
}

func validateAgentURL(raw string) error {
	if strings.ContainsAny(raw, "?#") {
		return fmt.Errorf("must be https://<fqdn>.ts.net:9474")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Path != "" || u.RawPath != "" || u.Opaque != "" || u.Port() != "9474" {
		return fmt.Errorf("must be https://<fqdn>.ts.net:9474")
	}
	host := u.Hostname()
	if !validDNSName(host) || !strings.HasSuffix(host, ".ts.net") || strings.TrimSuffix(host, ".ts.net") == "" {
		return fmt.Errorf("must be https://<fqdn>.ts.net:9474")
	}
	return nil
}

func validDNSName(host string) bool {
	if len(host) < 1 || len(host) > 253 || host != strings.ToLower(host) {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

func readAgentConfig(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open agent configuration: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, fmt.Errorf("inspect agent configuration: %w", err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0o777 != 0o640 {
		return nil, fmt.Errorf("agent configuration must be a regular file with mode 0640")
	}
	b, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil {
		return nil, fmt.Errorf("read agent configuration: %w", err)
	}
	if len(b) > 64*1024 {
		return nil, invalidAgent("agent configuration exceeds 64 KiB", nil)
	}
	return b, nil
}
