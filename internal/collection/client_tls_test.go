package collection

import (
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/agent"
)

type fixedResolver struct{ addresses []netip.Addr }

func (r fixedResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.addresses, nil
}

type mappedDialer struct{ target string }

func (d mappedDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

func TestClientTLSAuthSNIProxyRedirectAndPayload(t *testing.T) {
	credential := make([]byte, 32)
	for i := range credential {
		credential[i] = byte(i)
	}
	expectedAuth := "Bearer " + base64.RawURLEncoding.EncodeToString(credential)
	var sni, responseCase string
	server, roots := newAgentTLSServer(t, "oracle.example.ts.net", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sni = r.TLS.ServerName
		if r.Header.Get("Authorization") != expectedAuth {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch responseCase {
		case "redirect":
			http.Redirect(w, r, "https://oracle.example.ts.net:9474/agent/v1/snapshot", http.StatusFound)
		case "large":
			w.Header().Set("Content-Encoding", "gzip")
			zip := gzip.NewWriter(w)
			_, _ = zip.Write([]byte(strings.Repeat("x", MaxPayloadBytes+1)))
			_ = zip.Close()
		default:
			_ = json.NewEncoder(w).Encode(minimalWireSnapshot())
		}
	}))
	defer server.Close()
	client, err := NewClient(fixedResolver{[]netip.Addr{netip.MustParseAddr("100.64.0.10")}}, mappedDialer{server.Listener.Addr().String()}, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	claim := Claim{AgentURL: "https://oracle.example.ts.net:9474", Credential: credential, Timeout: time.Second}
	oldHTTP, hadHTTP := os.LookupEnv("HTTPS_PROXY")
	_ = os.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	defer func() {
		if hadHTTP {
			_ = os.Setenv("HTTPS_PROXY", oldHTTP)
		} else {
			_ = os.Unsetenv("HTTPS_PROXY")
		}
	}()
	if _, err = client.Pull(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if sni != "oracle.example.ts.net" {
		t.Fatalf("SNI=%q", sni)
	}
	responseCase = "redirect"
	if _, err = client.Pull(context.Background(), claim); failure(err) != FailureProtocol {
		t.Fatalf("redirect err=%v", err)
	}
	responseCase = "large"
	if _, err = client.Pull(context.Background(), claim); failure(err) != FailurePayload {
		t.Fatalf("compressed payload err=%v", err)
	}
}

func TestClientClassifiesAuthTimeoutPayloadAndProtocol(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    FailureKind
		timeout time.Duration
	}{{"auth", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }, FailureAuth, time.Second}, {"timeout", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, FailureTimeout, 20 * time.Millisecond}, {"payload", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", MaxPayloadBytes+1)))
	}, FailurePayload, time.Second}, {"protocol", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"protocol_version":1,"unknown":true}`))
	}, FailureProtocol, time.Second}, {"proxy-unavailable", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}, FailureConnect, time.Second}}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			server, roots := newAgentTLSServer(t, "oracle.example.ts.net", item.handler)
			defer server.Close()
			client, _ := NewClient(fixedResolver{[]netip.Addr{netip.MustParseAddr("fd7a:115c:a1e0::1")}}, mappedDialer{server.Listener.Addr().String()}, &tls.Config{RootCAs: roots})
			_, err := client.Pull(context.Background(), Claim{AgentURL: "https://oracle.example.ts.net:9474", Credential: make([]byte, 32), Timeout: item.timeout})
			if failure(err) != item.want {
				t.Fatalf("kind=%s err=%v", failure(err), err)
			}
			if strings.Contains(err.Error(), base64.RawURLEncoding.EncodeToString(make([]byte, 32))) {
				t.Fatal("credential leaked in error")
			}
		})
	}
}

func TestClientRejectsOmittedAndNullRequiredArrays(t *testing.T) {
	credential := make([]byte, 32)
	var payload []byte
	server, roots := newAgentTLSServer(t, "oracle.example.ts.net", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	client, _ := NewClient(fixedResolver{[]netip.Addr{netip.MustParseAddr("100.64.0.10")}}, mappedDialer{server.Listener.Addr().String()}, &tls.Config{RootCAs: roots})
	claim := Claim{AgentURL: "https://oracle.example.ts.net:9474", Credential: credential, Timeout: time.Second}

	for _, field := range []string{"tailscale.addresses", "filesystems", "collection_warnings"} {
		for _, mode := range []string{"omitted", "null"} {
			t.Run(field+"/"+mode, func(t *testing.T) {
				var document map[string]any
				encoded, _ := json.Marshal(minimalWireSnapshot())
				_ = json.Unmarshal(encoded, &document)
				target := document
				key := field
				if field == "tailscale.addresses" {
					target = document["tailscale"].(map[string]any)
					key = "addresses"
				}
				if mode == "omitted" {
					delete(target, key)
				} else {
					target[key] = nil
				}
				payload, _ = json.Marshal(document)
				if _, err := client.Pull(context.Background(), claim); failure(err) != FailureProtocol {
					t.Fatalf("kind=%s err=%v", failure(err), err)
				}
			})
		}
	}
	payload, _ = json.Marshal(minimalWireSnapshot())
	if _, err := client.Pull(context.Background(), claim); err != nil {
		t.Fatalf("explicit empty arrays rejected: %v", err)
	}
}

func TestClientRequiresEveryExactWireField(t *testing.T) {
	var payload []byte
	server, roots := newAgentTLSServer(t, "oracle.example.ts.net", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(payload) }))
	defer server.Close()
	client, _ := NewClient(fixedResolver{[]netip.Addr{netip.MustParseAddr("100.64.0.10")}}, mappedDialer{server.Listener.Addr().String()}, &tls.Config{RootCAs: roots})
	claim := Claim{AgentURL: "https://oracle.example.ts.net:9474", Credential: make([]byte, 32), Timeout: time.Second}
	paths := [][]string{
		{"protocol_version"}, {"agent_version"}, {"host"}, {"tailscale"}, {"filesystems"}, {"collection_warnings"},
		{"host", "hostname"}, {"host", "observed_at"}, {"host", "boot_id"}, {"host", "uptime_seconds"}, {"host", "cpu_used_percent"}, {"host", "memory_used_bytes"}, {"host", "memory_total_bytes"},
		{"tailscale", "state"}, {"tailscale", "hostname"}, {"tailscale", "addresses"},
		{"filesystems", "0", "mount_point"}, {"filesystems", "0", "device"}, {"filesystems", "0", "fs_uuid"}, {"filesystems", "0", "mount_root"}, {"filesystems", "0", "major_minor"}, {"filesystems", "0", "fs_type"}, {"filesystems", "0", "used_bytes"}, {"filesystems", "0", "total_bytes"}, {"filesystems", "0", "available_bytes"},
	}
	for _, path := range paths {
		t.Run(strings.Join(path, "."), func(t *testing.T) {
			snapshot := minimalWireSnapshot()
			snapshot.Filesystems = []agent.Filesystem{validFilesystem()}
			encoded, _ := json.Marshal(snapshot)
			var document map[string]any
			_ = json.Unmarshal(encoded, &document)
			deleteJSONPath(document, path)
			payload, _ = json.Marshal(document)
			if _, err := client.Pull(context.Background(), claim); failure(err) != FailureProtocol {
				t.Fatalf("omitted field accepted: %v", err)
			}
		})
	}
}

func TestClientRejectsDuplicateMembersRegardlessOfOrder(t *testing.T) {
	var payload []byte
	server, roots := newAgentTLSServer(t, "oracle.example.ts.net", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(payload) }))
	defer server.Close()
	client, _ := NewClient(fixedResolver{[]netip.Addr{netip.MustParseAddr("100.64.0.10")}}, mappedDialer{server.Listener.Addr().String()}, &tls.Config{RootCAs: roots})
	claim := Claim{AgentURL: "https://oracle.example.ts.net:9474", Credential: make([]byte, 32), Timeout: time.Second}
	base, _ := json.Marshal(minimalWireSnapshot())
	valid := string(base)
	cases := map[string]string{
		"root valid then invalid":   strings.Replace(valid, `"protocol_version":1`, `"protocol_version":1,"protocol_version":2`, 1),
		"root invalid then valid":   strings.Replace(valid, `"protocol_version":1`, `"protocol_version":2,"protocol_version":1`, 1),
		"nested valid then invalid": strings.Replace(valid, `"hostname":"host"`, `"hostname":"host","hostname":""`, 1),
		"nested invalid then valid": strings.Replace(valid, `"hostname":"host"`, `"hostname":"","hostname":"host"`, 1),
	}
	filesystem := validFilesystem()
	withFilesystem := minimalWireSnapshot()
	withFilesystem.Filesystems = []agent.Filesystem{filesystem}
	encodedFilesystem, _ := json.Marshal(withFilesystem)
	cases["array object duplicate"] = strings.Replace(string(encodedFilesystem), `"used_bytes":0`, `"used_bytes":0,"used_bytes":1`, 1)
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			payload = []byte(document)
			if _, err := client.Pull(context.Background(), claim); failure(err) != FailureProtocol {
				t.Fatalf("duplicate accepted: %v", err)
			}
		})
	}
}

func deleteJSONPath(document map[string]any, path []string) {
	var current any = document
	for _, part := range path[:len(path)-1] {
		switch value := current.(type) {
		case map[string]any:
			current = value[part]
		case []any:
			current = value[0]
		}
	}
	delete(current.(map[string]any), path[len(path)-1])
}

func failure(err error) FailureKind {
	var pull *PullError
	if errors.As(err, &pull) {
		return pull.Kind
	}
	return ""
}

func newAgentTLSServer(t *testing.T, host string, handler http.Handler) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	roots := x509.NewCertPool()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(parsed)
	return server, roots
}
