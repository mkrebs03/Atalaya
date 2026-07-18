package agent

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCredentialPermissionsAndSymlink(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "secret")
	encoded := base64.RawURLEncoding.EncodeToString(make([]byte, 32)) + "\n"
	if err := os.WriteFile(p, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCredential(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCredential(p); err == nil {
		t.Fatal("accepted mode 0640")
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(d, "link")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCredential(link); err == nil {
		t.Fatal("accepted symlink")
	}
}
func TestLoadCredentialRejectsWhitespaceAndPadding(t *testing.T) {
	for _, value := range []string{" " + string(make([]byte, 43)), base64.RawURLEncoding.EncodeToString(make([]byte, 32)) + "\r\n", base64.URLEncoding.EncodeToString(make([]byte, 32))} {
		p := filepath.Join(t.TempDir(), "s")
		if err := os.WriteFile(p, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCredential(p); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}
