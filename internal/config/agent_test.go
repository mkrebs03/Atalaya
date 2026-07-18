package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAgentStrictAndResolvesCredential(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "agent.yaml")
	valid := "listen: 127.0.0.1:9474\nserver_url: https://oracle.ts.net\nadvertised_url: https://oracle.ts.net:9474\ncredential_path: secret\n"
	if err := os.WriteFile(p, []byte(valid), 0o640); err != nil {
		t.Fatal(err)
	}
	c, err := LoadAgent(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.CredentialPath != filepath.Join(d, "secret") {
		t.Fatalf("path=%q", c.CredentialPath)
	}
	if err := os.WriteFile(p, []byte(valid+"unknown: true\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	_, err = LoadAgent(p)
	if !errors.Is(err, ErrInvalidAgentConfig) {
		t.Fatalf("err=%v", err)
	}
}
func TestAgentConfigRejectsNonLoopbackAndInvalidAdvertisedURL(t *testing.T) {
	for _, c := range []AgentConfig{{Listen: "0.0.0.0:9474", ServerURL: "https://oracle.ts.net", AdvertisedURL: "https://oracle.ts.net:9474", CredentialPath: "x"}, {Listen: "127.0.0.1:9474", ServerURL: "https://oracle.ts.net", AdvertisedURL: "https://example.com:9474", CredentialPath: "x"}} {
		if c.Validate() == nil {
			t.Fatalf("accepted %#v", c)
		}
	}
}

func TestLoadAgentRejectsModeSymlinkAndOversize(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "agent.yaml")
	body := []byte("listen: 127.0.0.1:9474\nserver_url: https://oracle.ts.net\nadvertised_url: https://oracle.ts.net:9474\ncredential_path: secret\n")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAgent(p); err == nil {
		t.Fatal("accepted mode 0600")
	}
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(d, "link")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAgent(link); err == nil {
		t.Fatal("accepted symlink")
	}
	if err := os.WriteFile(filepath.Join(d, "large"), make([]byte, 64*1024+1), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAgent(filepath.Join(d, "large")); err == nil {
		t.Fatal("accepted oversized config")
	}
}
func TestAdvertisedURLRequiresStrictDNSName(t *testing.T) {
	for _, host := range []string{"UPPER.ts.net", "under_score.ts.net", "-bad.ts.net", "bad-.ts.net", "a..ts.net"} {
		c := AgentConfig{Listen: "127.0.0.1:9474", ServerURL: "https://oracle.ts.net", AdvertisedURL: "https://" + host + ":9474", CredentialPath: "x"}
		if c.Validate() == nil {
			t.Fatalf("accepted %q", host)
		}
	}
}
