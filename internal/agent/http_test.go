package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type staticCollector struct{ s Snapshot }

func (c staticCollector) Collect(context.Context, time.Time) Snapshot { return c.s }
func TestHandlerAuthAndShape(t *testing.T) {
	secret := make([]byte, 32)
	h, err := NewHandler(secret, "dev", staticCollector{Snapshot{Host: Host{Hostname: "host", ObservedAt: "2026-07-17T00:00:00Z"}, Tailscale: unknownTailscale()}})
	if err != nil {
		t.Fatal(err)
	}
	unauth := httptest.NewRecorder()
	h.ServeHTTP(unauth, httptest.NewRequest("GET", "/agent/v1/snapshot", nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", unauth.Code)
	}
	r := httptest.NewRequest("GET", "/agent/v1/snapshot", nil)
	r.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(secret))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var raw map[string]any
	if json.Unmarshal(w.Body.Bytes(), &raw) != nil {
		t.Fatal("invalid JSON")
	}
	for _, key := range []string{"protocol_version", "agent_version", "host", "tailscale", "filesystems", "collection_warnings"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing %s", key)
		}
	}
}
func TestWarningInvariant(t *testing.T) {
	if validWarnings([]string{"filesystems_partial", "filesystems_unavailable"}) {
		t.Fatal("accepted mutually exclusive warnings")
	}
	if validWarnings([]string{"cpu_unavailable", "cpu_unavailable"}) {
		t.Fatal("accepted duplicate")
	}
}
func TestLimiterBurstAndRefill(t *testing.T) {
	b := newTokenBucket(10, time.Second)
	now := time.Unix(1, 0)
	for i := 0; i < 10; i++ {
		if !b.Allow(now) {
			t.Fatal("burst rejected")
		}
	}
	if b.Allow(now) {
		t.Fatal("eleventh accepted")
	}
	if !b.Allow(now.Add(time.Second)) {
		t.Fatal("refill rejected")
	}
}

func validSnapshot() Snapshot {
	return Snapshot{Host: Host{Hostname: "host", ObservedAt: "2026-07-17T00:00:00Z"}, Tailscale: unknownTailscale(), Filesystems: []Filesystem{}, CollectionWarnings: []string{}}
}
func authorizedRequest(secret []byte) *http.Request {
	r := httptest.NewRequest("GET", "/agent/v1/snapshot", nil)
	r.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(secret))
	return r
}
func TestHealthMethodAndBodies(t *testing.T) {
	h, _ := NewHandler(make([]byte, 32), "dev", staticCollector{validSnapshot()})
	for _, tc := range []struct {
		method string
		body   []byte
		want   int
	}{{"GET", nil, 200}, {"POST", nil, 405}, {"GET", []byte("x"), 400}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, "/agent/v1/healthz", bytes.NewReader(tc.body)))
		if w.Code != tc.want {
			t.Fatalf("%s body=%q status=%d", tc.method, tc.body, w.Code)
		}
	}
}
func TestMalformedBearerAndInvalidSnapshotAreSafe(t *testing.T) {
	secret := make([]byte, 32)
	h, _ := NewHandler(secret, "dev", staticCollector{validSnapshot()})
	for _, value := range []string{"Bearer !!!", "bearer " + base64.RawURLEncoding.EncodeToString(secret), "Bearer x y"} {
		r := httptest.NewRequest("GET", "/agent/v1/snapshot", nil)
		r.Header.Set("Authorization", value)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("auth %q status=%d", value, w.Code)
		}
	}
	bad := validSnapshot()
	bad.Host.Hostname = ""
	h, _ = NewHandler(secret, "dev", staticCollector{bad})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, authorizedRequest(secret))
	if w.Code != 503 || bytes.Contains(w.Body.Bytes(), []byte("hostname")) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandlerRejectsSharedWireInvariants(t *testing.T) {
	secret := make([]byte, 32)
	mutations := []func(*Snapshot){
		func(s *Snapshot) { s.Tailscale.Addresses = []string{"100.64.0.1", "100.64.0.1"} },
		func(s *Snapshot) { s.CollectionWarnings = []string{"cpu_unavailable", "cpu_unavailable"} },
		func(s *Snapshot) {
			fs := Filesystem{MountPoint: "/", MountRoot: "/", Device: "/dev/sda1", MajorMinor: "8:1", FSType: "ext4", TotalBytes: 1}
			s.Filesystems = []Filesystem{fs, fs}
		},
		func(s *Snapshot) {
			s.Filesystems = []Filesystem{{MountPoint: "/", MountRoot: "/", Device: "/dev//sda1", MajorMinor: "8:1", FSType: "ext4", TotalBytes: 1}}
		},
	}
	for index, mutate := range mutations {
		snapshot := validSnapshot()
		mutate(&snapshot)
		handler, _ := NewHandler(secret, "dev", staticCollector{snapshot})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, authorizedRequest(secret))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("mutation %d status=%d", index, response.Code)
		}
	}
}

type blockingCollector struct {
	started chan struct{}
	release chan struct{}
}

func (c blockingCollector) Collect(context.Context, time.Time) Snapshot {
	close(c.started)
	<-c.release
	return validSnapshot()
}
func TestOnlyOneCollectionInFlight(t *testing.T) {
	secret := make([]byte, 32)
	c := blockingCollector{make(chan struct{}), make(chan struct{})}
	h, _ := NewHandler(secret, "dev", c)
	done := make(chan int)
	go func() { w := httptest.NewRecorder(); h.ServeHTTP(w, authorizedRequest(secret)); done <- w.Code }()
	<-c.started
	w := httptest.NewRecorder()
	h.ServeHTTP(w, authorizedRequest(secret))
	if w.Code != 503 {
		t.Fatalf("concurrent status=%d", w.Code)
	}
	close(c.release)
	if code := <-done; code != 200 {
		t.Fatalf("first status=%d", code)
	}
}
func TestOversizedSnapshotRejected(t *testing.T) {
	secret := make([]byte, 32)
	s := validSnapshot()
	for i := 0; i < 100; i++ {
		suffix := strings.Repeat("a", 3000) + strconv.Itoa(i)
		s.Filesystems = append(s.Filesystems, Filesystem{MountPoint: "/" + suffix, MountRoot: "/", Device: "/dev/" + suffix, MajorMinor: "8:1", FSType: "ext4", TotalBytes: 1})
	}
	h, _ := NewHandler(secret, "dev", staticCollector{s})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, authorizedRequest(secret))
	if w.Code != 503 {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestAgentVersionRejectsControlsAndInvalidUTF8(t *testing.T) {
	secret := make([]byte, 32)
	for _, version := range []string{"bad\nversion", string([]byte{0xff})} {
		if _, err := NewHandler(secret, version, staticCollector{validSnapshot()}); err == nil {
			t.Fatalf("accepted version %q", version)
		}
	}
	bad := validSnapshot()
	bad.AgentVersion = "bad\nversion"
	bad.ProtocolVersion = 1
	if validateSnapshot(bad) == nil {
		t.Fatal("validator accepted control version")
	}
}
