package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	startupconfig "github.com/mkrebs03/atalaya/internal/config"
)

func TestEnrollPersistsReplayStateAndCredentialSafely(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	credential := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	var attempts int
	var firstRequest map[string]any
	server := newIPv4TestServer(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		attempts++
		if err := json.NewDecoder(request.Body).Decode(&firstRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if attempts == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"host_id":"host-1","agent_credential":"` + credential + `","poll_interval_seconds":60}`))
	}))

	directory := t.TempDir()
	credentialPath := filepath.Join(directory, "credential")
	statePath := filepath.Join(directory, "enrollment-state")
	options := EnrollmentOptions{
		Config:      startupconfig.AgentConfig{ServerURL: server.URL, AdvertisedURL: "https://remote.example.ts.net:9474", CredentialPath: credentialPath},
		DisplayName: "Remote",
		Token:       token,
		Version:     "dev",
		StatePath:   statePath,
		Random:      bytes.NewReader(bytes.Repeat([]byte{3}, 64)),
	}
	if _, err := Enroll(context.Background(), options); err == nil {
		t.Fatal("first failed response was accepted")
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state sidecar missing after failed response: %v", err)
	}
	hostID, err := Enroll(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if hostID != "host-1" || attempts != 2 {
		t.Fatalf("host=%q attempts=%d", hostID, attempts)
	}
	if firstRequest["enrollment_token"] != token || firstRequest["request_id"] == "" {
		t.Fatalf("request=%#v", firstRequest)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state sidecar remains: %v", err)
	}
	loaded, err := LoadCredential(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded, bytes.Repeat([]byte{9}, 32)) {
		t.Fatal("credential differs from server response")
	}
	clear(loaded)
	info, err := os.Stat(credentialPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential permissions = %v, err=%v", info.Mode().Perm(), err)
	}
}

func newIPv4TestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}
