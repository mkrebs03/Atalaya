package hosts

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mkrebs03/atalaya/internal/config"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/unix"
)

const (
	DefaultDisplayName = "Oracle"
	DefaultListen      = "127.0.0.1:9474"
	CredentialBytes    = 32
)

var (
	ErrBootstrapMismatch       = errors.New("existing local-agent bootstrap files do not match requested configuration")
	ErrInvalidBootstrapRequest = errors.New("invalid local-agent bootstrap request")
)

type LocalHost struct {
	ID                 string
	Credential         []byte
	BootstrapCompleted bool
}

type LocalHostRepository interface {
	PrepareLocalHost(context.Context, string, string, time.Time, io.Reader) (LocalHost, error)
	CompleteLocalHost(context.Context, string, time.Time, io.Reader) error
}

type BootstrapRequest struct {
	DisplayName    string
	Listen         string
	ServerURL      string
	AgentURL       string
	AgentConfig    string
	CredentialPath string
}

type Bootstrapper struct {
	Repository LocalHostRepository
	Clock      func() time.Time
	Random     io.Reader
	// Boundary is an optional failure-injection hook used to prove recovery.
	// Production callers leave it nil.
	Boundary func(string) error
}

func (b Bootstrapper) Run(ctx context.Context, request BootstrapRequest) (string, error) {
	if b.Repository == nil {
		return "", fmt.Errorf("local host repository is required")
	}
	if b.Clock == nil {
		b.Clock = time.Now
	}
	if b.Random == nil {
		b.Random = rand.Reader
	}
	request, agentConfig, _, err := normalizeRequest(request)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidBootstrapRequest, err)
	}
	host, err := b.Repository.PrepareLocalHost(ctx, request.DisplayName, request.AgentURL, b.Clock().UTC(), b.Random)
	if err != nil {
		return "", err
	}
	defer clear(host.Credential)
	if err := b.boundary("database_prepared"); err != nil {
		return "", err
	}
	if len(host.Credential) != CredentialBytes {
		return "", fmt.Errorf("stored local-agent credential is invalid")
	}
	credentialText := base64.RawURLEncoding.EncodeToString(host.Credential) + "\n"
	if err := ensureCredential(request.CredentialPath, []byte(credentialText), host.Credential); err != nil {
		return "", err
	}
	if err := b.boundary("credential_ready"); err != nil {
		return "", err
	}
	if err := ensureAgentConfig(request.AgentConfig, agentConfig); err != nil {
		return "", err
	}
	if err := b.boundary("configuration_ready"); err != nil {
		return "", err
	}
	if !host.BootstrapCompleted {
		if err := b.boundary("before_completion"); err != nil {
			return "", err
		}
		if err := b.Repository.CompleteLocalHost(ctx, host.ID, b.Clock().UTC(), b.Random); err != nil {
			return "", err
		}
		if err := b.boundary("after_completion"); err != nil {
			return "", err
		}
	}
	return host.ID, nil
}

func (b Bootstrapper) boundary(name string) error {
	if b.Boundary == nil {
		return nil
	}
	if err := b.Boundary(name); err != nil {
		return fmt.Errorf("bootstrap interrupted after %s: %w", name, err)
	}
	return nil
}

func normalizeRequest(r BootstrapRequest) (BootstrapRequest, config.AgentConfig, string, error) {
	if r.DisplayName == "" {
		r.DisplayName = DefaultDisplayName
	}
	if r.Listen == "" {
		r.Listen = DefaultListen
	}
	if strings.TrimSpace(r.DisplayName) == "" || r.AgentConfig == "" || r.AgentURL == "" || r.ServerURL == "" {
		return r, config.AgentConfig{}, "", fmt.Errorf("display name, server URL, agent URL, and agent config are required")
	}
	absConfig, err := filepath.Abs(r.AgentConfig)
	if err != nil {
		return r, config.AgentConfig{}, "", fmt.Errorf("resolve agent config path: %w", err)
	}
	r.AgentConfig = filepath.Clean(absConfig)
	configCredentialPath := r.CredentialPath
	if configCredentialPath == "" {
		configCredentialPath = "agent-credential"
		r.CredentialPath = filepath.Join(filepath.Dir(r.AgentConfig), configCredentialPath)
	} else {
		if !filepath.IsAbs(r.CredentialPath) {
			r.CredentialPath = filepath.Join(filepath.Dir(r.AgentConfig), r.CredentialPath)
		}
		r.CredentialPath = filepath.Clean(r.CredentialPath)
	}
	canonicalServer, err := canonicalOrigin(r.ServerURL)
	if err != nil {
		return r, config.AgentConfig{}, "", fmt.Errorf("server URL: %w", err)
	}
	canonicalAgent, err := canonicalOrigin(r.AgentURL)
	if err != nil {
		return r, config.AgentConfig{}, "", fmt.Errorf("agent URL: %w", err)
	}
	r.ServerURL, r.AgentURL = canonicalServer, canonicalAgent
	c := config.AgentConfig{Listen: r.Listen, ServerURL: r.ServerURL, AdvertisedURL: r.AgentURL, CredentialPath: configCredentialPath}
	if err := c.Validate(); err != nil {
		return r, config.AgentConfig{}, "", fmt.Errorf("invalid agent configuration: %w", err)
	}
	return r, c, configCredentialPath, nil
}

func canonicalOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	return u.String(), nil
}

func ensureCredential(path string, contents, raw []byte) error {
	verify := func() error { return verifyCredential(path, raw) }
	if err := verify(); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return createAtomic(path, contents, 0o600, verify)
}

func verifyCredential(path string, raw []byte) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return os.ErrNotExist
		}
		return fmt.Errorf("open credential: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("inspect credential: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o777 != 0o600 {
		return fmt.Errorf("credential must be a regular file with mode 0600")
	}
	loaded, err := io.ReadAll(io.LimitReader(file, 45))
	if err != nil {
		return fmt.Errorf("read credential: %w", err)
	}
	if len(loaded) == 44 && loaded[43] == '\n' {
		loaded = loaded[:43]
	}
	if len(loaded) != 43 {
		return ErrBootstrapMismatch
	}
	decoded, err := base64.RawURLEncoding.DecodeString(string(loaded))
	if err != nil || len(decoded) != CredentialBytes || !equalBytes(decoded, raw) {
		return ErrBootstrapMismatch
	}
	return nil
}

func ensureAgentConfig(path string, desired config.AgentConfig) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o640 {
			return fmt.Errorf("agent configuration must be a regular file with mode 0640")
		}
		loaded, err := config.LoadAgent(path)
		if err != nil {
			return fmt.Errorf("read existing agent configuration: %w", err)
		}
		desiredCredential := desired.CredentialPath
		if !filepath.IsAbs(desiredCredential) {
			desiredCredential = filepath.Join(filepath.Dir(path), desiredCredential)
		}
		loadedServer, serverErr := canonicalOrigin(loaded.ServerURL)
		loadedAgent, agentErr := canonicalOrigin(loaded.AdvertisedURL)
		if serverErr != nil || agentErr != nil || loaded.Listen != desired.Listen || loadedServer != desired.ServerURL || loadedAgent != desired.AdvertisedURL || loaded.CredentialPath != filepath.Clean(desiredCredential) {
			return ErrBootstrapMismatch
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect agent configuration: %w", err)
	}
	body, err := yaml.Marshal(desired)
	if err != nil {
		return fmt.Errorf("encode agent configuration: %w", err)
	}
	verify := func() error { return verifyAgentConfig(path, desired) }
	return createAtomic(path, body, 0o640, verify)
}

func verifyAgentConfig(path string, desired config.AgentConfig) error {
	loaded, err := config.LoadAgent(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		return err
	}
	desiredCredential := desired.CredentialPath
	if !filepath.IsAbs(desiredCredential) {
		desiredCredential = filepath.Join(filepath.Dir(path), desiredCredential)
	}
	loadedServer, serverErr := canonicalOrigin(loaded.ServerURL)
	loadedAgent, agentErr := canonicalOrigin(loaded.AdvertisedURL)
	if serverErr != nil || agentErr != nil || loaded.Listen != desired.Listen || loadedServer != desired.ServerURL || loadedAgent != desired.AdvertisedURL || loaded.CredentialPath != filepath.Clean(desiredCredential) {
		return ErrBootstrapMismatch
	}
	return nil
}

func createAtomic(path string, contents []byte, mode os.FileMode, verify func() error) error {
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, ".atalaya-bootstrap-"+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary %s: %w", filepath.Base(path), err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("set temporary %s permissions: %w", filepath.Base(path), err)
	}
	if _, err = temporary.Write(contents); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write temporary %s: %w", filepath.Base(path), err)
	}
	// A hard link installs the completed temporary file without replacing an
	// existing destination. This is portable across the supported Unix hosts;
	// the temporary file and destination are kept in the same directory.
	if err := os.Link(temporaryPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return verify()
		}
		return fmt.Errorf("install %s: %w", filepath.Base(path), err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open parent directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync parent directory: %w", err)
	}
	return nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var difference byte
	for i := range a {
		difference |= a[i] ^ b[i]
	}
	return difference == 0
}
