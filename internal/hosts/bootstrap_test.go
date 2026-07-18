package hosts

import (
	"bytes"
	"context"
	"fmt"
	"github.com/mkrebs03/atalaya/internal/config"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type bootstrapRepository struct {
	host                LocalHost
	prepared, completed int
}

func TestCredentialVerifierRejectsFIFOOversizeAndSymlink(t *testing.T) {
	raw := bytes.Repeat([]byte{4}, 32)
	t.Run("fifo", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "secret")
		if err := unix.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := verifyCredential(path, raw); err == nil {
			t.Fatal("FIFO accepted")
		}
	})
	t.Run("oversize", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(path, bytes.Repeat([]byte{'a'}, 45), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := verifyCredential(path, raw); err == nil {
			t.Fatal("oversize accepted")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "secret")
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if err := verifyCredential(path, raw); err == nil {
			t.Fatal("symlink accepted")
		}
	})
}

func TestGeneratedYAMLRoundTripsQuotedValues(t *testing.T) {
	dir := t.TempDir()
	desired := config.AgentConfig{Listen: "127.0.0.1:9474", ServerURL: "http://localhost", AdvertisedURL: "https://oracle.example.ts.net:9474", CredentialPath: "secret # one:two"}
	if err := ensureAgentConfig(filepath.Join(dir, "agent.yaml"), desired); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadAgent(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CredentialPath != filepath.Join(dir, "secret # one:two") {
		t.Fatalf("credential path=%q", loaded.CredentialPath)
	}
}

func TestCanonicalOriginNormalizesEquivalentOrigins(t *testing.T) {
	for raw, want := range map[string]string{"HTTP://Example.COM:80": "http://example.com", "https://Example.COM:443": "https://example.com", "https://[FD00::1]:443": "https://[fd00::1]", "https://Example.COM:9474": "https://example.com:9474"} {
		got, err := canonicalOrigin(raw)
		if err != nil || got != want {
			t.Fatalf("%q => %q err=%v want=%q", raw, got, err, want)
		}
	}
}

type repositoryAdapter struct{ *bootstrapRepository }

func (r repositoryAdapter) PrepareLocalHost(_ context.Context, _, _ string, _ time.Time, _ io.Reader) (LocalHost, error) {
	r.prepared++
	return LocalHost{ID: r.host.ID, Credential: append([]byte(nil), r.host.Credential...), BootstrapCompleted: r.host.BootstrapCompleted}, nil
}

type ownershipRepository struct{ credential []byte }

func (r *ownershipRepository) PrepareLocalHost(context.Context, string, string, time.Time, io.Reader) (LocalHost, error) {
	return LocalHost{ID: "host", Credential: r.credential}, nil
}
func (*ownershipRepository) CompleteLocalHost(context.Context, string, time.Time, io.Reader) error {
	return nil
}

func TestBootstrapperClearsTransferredCredentialOnSuccessAndFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprintf("failure_%t", failure), func(t *testing.T) {
			dir := t.TempDir()
			credential := bytes.Repeat([]byte{0x6c}, 32)
			repository := &ownershipRepository{credential: credential}
			request := BootstrapRequest{AgentConfig: filepath.Join(dir, "agent.yaml"), AgentURL: "https://oracle.example.ts.net:9474", ServerURL: "http://localhost"}
			if failure {
				if err := os.WriteFile(filepath.Join(dir, "agent-credential"), []byte("mismatch"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := (Bootstrapper{Repository: repository}).Run(context.Background(), request)
			if failure && err == nil {
				t.Fatal("expected failure")
			}
			if !failure && err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(credential, make([]byte, len(credential))) {
				t.Fatal("caller-owned credential was not cleared")
			}
		})
	}
}
func (r repositoryAdapter) CompleteLocalHost(_ context.Context, _ string, _ time.Time, _ io.Reader) error {
	r.completed++
	r.host.BootstrapCompleted = true
	return nil
}

func TestBootstrapCreatesAndVerifiesFilesWithoutRotating(t *testing.T) {
	dir := t.TempDir()
	record := &bootstrapRepository{host: LocalHost{ID: "host", Credential: bytes.Repeat([]byte{7}, 32)}}
	b := Bootstrapper{Repository: repositoryAdapter{record}, Clock: func() time.Time { return time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC) }, Random: bytes.NewReader(make([]byte, 128))}
	req := BootstrapRequest{AgentConfig: filepath.Join(dir, "agent.yaml"), AgentURL: "https://oracle.example.ts.net:9474", ServerURL: "http://127.0.0.1:8080"}
	if _, err := b.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{filepath.Join(dir, "agent.yaml"): 0o640, filepath.Join(dir, "agent-credential"): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode=%o", path, info.Mode().Perm())
		}
	}
	if record.prepared != 2 || record.completed != 1 {
		t.Fatalf("calls prepare=%d complete=%d", record.prepared, record.completed)
	}
}

func TestBootstrapRecoversAfterEveryExternalBoundary(t *testing.T) {
	for _, boundary := range []string{"database_prepared", "credential_ready", "configuration_ready"} {
		t.Run(boundary, func(t *testing.T) {
			dir := t.TempDir()
			record := &bootstrapRepository{host: LocalHost{ID: "host", Credential: bytes.Repeat([]byte{9}, 32)}}
			failed := false
			bootstrapper := Bootstrapper{Repository: repositoryAdapter{record}, Boundary: func(observed string) error {
				if observed == boundary && !failed {
					failed = true
					return os.ErrClosed
				}
				return nil
			}}
			request := BootstrapRequest{AgentConfig: filepath.Join(dir, "agent.yaml"), AgentURL: "https://oracle.example.ts.net:9474", ServerURL: "http://127.0.0.1:8080"}
			if _, err := bootstrapper.Run(context.Background(), request); err == nil {
				t.Fatal("injected interruption did not fail")
			}
			bootstrapper.Boundary = nil
			if _, err := bootstrapper.Run(context.Background(), request); err != nil {
				t.Fatalf("recovery: %v", err)
			}
			if record.completed != 1 {
				t.Fatalf("completion calls=%d", record.completed)
			}
		})
	}
}

func TestBootstrapRejectsExistingMismatch(t *testing.T) {
	dir := t.TempDir()
	credential := filepath.Join(dir, "agent-credential")
	if err := os.WriteFile(credential, []byte("wrong"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := &bootstrapRepository{host: LocalHost{ID: "host", Credential: bytes.Repeat([]byte{7}, 32)}}
	_, err := (Bootstrapper{Repository: repositoryAdapter{record}}).Run(context.Background(), BootstrapRequest{AgentConfig: filepath.Join(dir, "agent.yaml"), AgentURL: "https://oracle.example.ts.net:9474", ServerURL: "http://127.0.0.1:8080"})
	if err == nil {
		t.Fatal("mismatch accepted")
	}
}
