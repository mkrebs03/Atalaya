package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	startupconfig "github.com/mkrebs03/atalaya/internal/config"
)

type EnrollmentOptions struct {
	Config      startupconfig.AgentConfig
	DisplayName string
	Token       string
	Version     string
	StatePath   string
	Client      *http.Client
	Random      io.Reader
}

type enrollmentState struct {
	RequestID    string `json:"request_id"`
	TokenHash    string `json:"token_hash"`
	DisplayName  string `json:"display_name"`
	AgentURL     string `json:"agent_url"`
	AgentVersion string `json:"agent_version"`
	Protocol     int    `json:"protocol_version"`
}

type enrollmentResponse struct {
	HostID          string `json:"host_id"`
	AgentCredential string `json:"agent_credential"`
	PollInterval    int    `json:"poll_interval_seconds"`
}

// Enroll performs the one-shot control-plane enrollment. The state sidecar
// contains no reusable secret; it only makes a lost HTTP response replayable.
func Enroll(ctx context.Context, options EnrollmentOptions) (string, error) {
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if options.Client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		options.Client = &http.Client{Timeout: 20 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if options.StatePath == "" {
		options.StatePath = options.Config.CredentialPath + ".enrollment-state"
	}
	if options.DisplayName == "" || options.Token == "" || options.Version == "" {
		return "", fmt.Errorf("display name, token, and agent version are required")
	}
	if len(options.Token) != 43 {
		return "", fmt.Errorf("enrollment token has invalid encoding")
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(options.Token); err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("enrollment token has invalid encoding")
	}
	tokenDigest := sha256.Sum256([]byte(options.Token))
	state := enrollmentState{TokenHash: base64.RawURLEncoding.EncodeToString(tokenDigest[:]), DisplayName: options.DisplayName, AgentURL: options.Config.AdvertisedURL, AgentVersion: options.Version, Protocol: ProtocolVersion}
	loaded, exists, err := loadEnrollmentState(options.StatePath)
	if err != nil {
		return "", err
	}
	if exists {
		if err := handoffToDirectoryOwner(options.StatePath); err != nil {
			return "", err
		}
		if loaded.TokenHash != state.TokenHash || loaded.DisplayName != state.DisplayName || loaded.AgentURL != state.AgentURL || loaded.AgentVersion != state.AgentVersion || loaded.Protocol != state.Protocol {
			return "", fmt.Errorf("enrollment state belongs to a different request")
		}
		state = loaded
	} else {
		var idBytes [32]byte
		if _, err := io.ReadFull(options.Random, idBytes[:]); err != nil {
			return "", fmt.Errorf("generate enrollment request ID: %w", err)
		}
		state.RequestID = base64.RawURLEncoding.EncodeToString(idBytes[:])
		if err := writeEnrollmentState(options.StatePath, state); err != nil {
			return "", err
		}
	}

	payload := struct {
		EnrollmentToken string `json:"enrollment_token"`
		RequestID       string `json:"request_id"`
		DisplayName     string `json:"display_name"`
		AgentURL        string `json:"agent_url"`
		AgentVersion    string `json:"agent_version"`
		ProtocolVersion int    `json:"protocol_version"`
	}{options.Token, state.RequestID, state.DisplayName, state.AgentURL, state.AgentVersion, state.Protocol}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(options.Config.ServerURL, "/")+"/api/v1/agent-enrollments", strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("create enrollment request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := options.Client.Do(request)
	if err != nil {
		return "", fmt.Errorf("enrollment request failed: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 4097)
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("enrollment request rejected with status %d", response.StatusCode)
	}
	var result enrollmentResponse
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return "", fmt.Errorf("decode enrollment response: %w", err)
	}
	if result.HostID == "" || len(result.AgentCredential) != 43 || result.PollInterval < 1 {
		return "", fmt.Errorf("enrollment response is invalid")
	}
	credential, err := base64.RawURLEncoding.DecodeString(result.AgentCredential)
	if err != nil || len(credential) != 32 {
		return "", fmt.Errorf("enrollment response contains invalid credential")
	}
	if err := writeCredentialNoReplace(options.Config.CredentialPath, credential); err != nil {
		clear(credential)
		return "", err
	}
	clear(credential)
	if err := os.Remove(options.StatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("remove enrollment state: %w", err)
	}
	return result.HostID, nil
}

func loadEnrollmentState(path string) (enrollmentState, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return enrollmentState{}, false, nil
	}
	if err != nil {
		return enrollmentState{}, false, fmt.Errorf("open enrollment state: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return enrollmentState{}, false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return enrollmentState{}, false, fmt.Errorf("enrollment state must be a regular file with mode 0600")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	var state enrollmentState
	if err := decoder.Decode(&state); err != nil {
		return enrollmentState{}, false, fmt.Errorf("decode enrollment state: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return enrollmentState{}, false, fmt.Errorf("decode enrollment state: unexpected trailing data")
	}
	if state.RequestID == "" || state.TokenHash == "" {
		return enrollmentState{}, false, fmt.Errorf("enrollment state is invalid")
	}
	return state, true, nil
}

func writeEnrollmentState(path string, state enrollmentState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("prepare enrollment state directory: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".atalaya-enrollment-*")
	if err != nil {
		return fmt.Errorf("create enrollment state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := handoffToDirectoryOwner(temporaryPath); err != nil {
		return err
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return fmt.Errorf("install enrollment state: %w", err)
	}
	return nil
}

// handoffToDirectoryOwner covers the documented sudo-based enrollment flow.
// Root may create the file, but the service account that owns the state
// directory must own the resulting credential and replay sidecar.
func handoffToDirectoryOwner(path string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("inspect enrollment state directory: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid == 0 {
		return nil
	}
	if err := os.Chown(path, int(stat.Uid), int(stat.Gid)); err != nil {
		return fmt.Errorf("handoff enrollment file ownership: %w", err)
	}
	return nil
}

func writeCredentialNoReplace(path string, credential []byte) error {
	encoded := append([]byte(base64.RawURLEncoding.EncodeToString(credential)), '\n')
	if existing, err := LoadCredential(path); err == nil {
		if subtle.ConstantTimeCompare(existing, credential) != 1 {
			clear(existing)
			return fmt.Errorf("existing agent credential does not match enrollment")
		}
		clear(existing)
		if err := handoffToDirectoryOwner(path); err != nil {
			return err
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) && !strings.Contains(err.Error(), "no such file") {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("prepare credential directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".atalaya-credential-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := handoffToDirectoryOwner(temporaryPath); err != nil {
		return err
	}
	if err := os.Link(temporaryPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return writeCredentialNoReplace(path, credential)
		}
		return fmt.Errorf("install agent credential: %w", err)
	}
	return nil
}
