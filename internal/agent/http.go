package agent

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mkrebs03/atalaya/internal/hosts"
)

const maxSnapshotBytes = 256 << 10

type Handler struct {
	credentialHash [32]byte
	collector      Collector
	version        string
	limiter        *tokenBucket
	collecting     chan struct{}
	now            func() time.Time
}

func NewHandler(credential []byte, version string, collector Collector) (*Handler, error) {
	if len(credential) != 32 {
		return nil, fmt.Errorf("agent credential must contain 32 bytes")
	}
	if collector == nil {
		return nil, fmt.Errorf("collector is required")
	}
	if !validText(version, 1, 64) {
		return nil, fmt.Errorf("agent version must contain 1 to 64 characters")
	}
	return &Handler{credentialHash: sha256.Sum256(credential), collector: collector, version: version, limiter: newTokenBucket(10, time.Second), collecting: make(chan struct{}, 1), now: time.Now}, nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	switch r.URL.Path {
	case "/agent/v1/healthz":
		if r.Method != "GET" {
			methodNotAllowed(w)
			return
		}
		if hasBody(r) {
			badRequest(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "/agent/v1/snapshot":
		h.snapshot(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]string{"code": "not_found", "message": "Resource not found"}})
	}
}
func (h *Handler) snapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		methodNotAllowed(w)
		return
	}
	if hasBody(r) {
		badRequest(w)
		return
	}
	if !h.authorized(r.Header.Get("Authorization")) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "authentication_required", "message": "Authentication required"}})
		return
	}
	if !h.limiter.Allow(h.now()) {
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]string{"code": "rate_limited", "message": "Too many requests"}})
		return
	}
	select {
	case h.collecting <- struct{}{}:
		defer func() { <-h.collecting }()
	case <-r.Context().Done():
		return
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]string{"code": "collection_in_progress", "message": "Collection already in progress"}})
		return
	}
	s := h.collector.Collect(r.Context(), h.now())
	s.ProtocolVersion = ProtocolVersion
	s.AgentVersion = h.version
	if s.Filesystems == nil {
		s.Filesystems = []Filesystem{}
	}
	if s.CollectionWarnings == nil {
		s.CollectionWarnings = []string{}
	}
	if s.Tailscale.Addresses == nil {
		s.Tailscale.Addresses = []string{}
	}
	if err := ValidateSnapshot(s); err != nil {
		collectionFailed(w)
		return
	}
	payload, err := json.Marshal(s)
	if err != nil || len(payload)+1 > maxSnapshotBytes {
		collectionFailed(w)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(payload, '\n'))
}
func (h *Handler) authorized(value string) bool {
	if !strings.HasPrefix(value, "Bearer ") || strings.Contains(value[7:], " ") {
		return false
	}
	raw, e := base64.RawURLEncoding.DecodeString(value[7:])
	if e != nil || len(raw) != 32 {
		return false
	}
	sum := sha256.Sum256(raw)
	return subtle.ConstantTimeCompare(sum[:], h.credentialHash[:]) == 1
}
func validWarnings(w []string) bool {
	seen := map[string]bool{}
	allowed := map[string]bool{"cpu_unavailable": true, "memory_unavailable": true, "uptime_unavailable": true, "boot_id_unavailable": true, "filesystems_partial": true, "filesystems_unavailable": true, "tailscale_unavailable": true}
	for _, v := range w {
		if !allowed[v] || seen[v] {
			return false
		}
		seen[v] = true
	}
	return !(seen["filesystems_partial"] && seen["filesystems_unavailable"])
}
func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET")
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": map[string]string{"code": "method_not_allowed", "message": "Method not allowed"}})
}
func badRequest(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_request", "message": "Invalid request"}})
}
func collectionFailed(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]string{"code": "collection_failed", "message": "Snapshot collection failed"}})
}
func hasBody(r *http.Request) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return false
	}
	var one [1]byte
	n, _ := r.Body.Read(one[:])
	return n != 0
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	last     time.Time
	period   time.Duration
}

func newTokenBucket(capacity int, period time.Duration) *tokenBucket {
	return &tokenBucket{tokens: float64(capacity), capacity: float64(capacity), period: period}
}
func (b *tokenBucket) Allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.last.IsZero() {
		b.last = now
	} else if now.After(b.last) {
		b.tokens += (float64(now.Sub(b.last)) / float64(b.period))
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func Run(ctx context.Context, listen string, handler http.Handler) error {
	listener, e := net.Listen("tcp", listen)
	if e != nil {
		return e
	}
	server := &http.Server{Addr: listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8 << 10}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case e := <-done:
		if e == http.ErrServerClosed {
			return nil
		}
		return e
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if e := server.Shutdown(shutdown); e != nil {
			_ = server.Close()
			return e
		}
		e := <-done
		if e == http.ErrServerClosed {
			return nil
		}
		return e
	}
}

// ValidateSnapshot checks the exact protocol-v1 snapshot contract.
func ValidateSnapshot(s Snapshot) error {
	if s.ProtocolVersion != 1 || !validText(s.AgentVersion, 1, 64) {
		return fmt.Errorf("invalid version")
	}
	if !validText(s.Host.Hostname, 1, 253) {
		return fmt.Errorf("invalid hostname")
	}
	observed, err := time.Parse(time.RFC3339Nano, s.Host.ObservedAt)
	if err != nil || observed.IsZero() {
		return fmt.Errorf("invalid observed time")
	}
	_, offset := observed.Zone()
	if offset != 0 {
		return fmt.Errorf("observed time must be UTC")
	}
	if s.Host.BootID != nil && !validText(*s.Host.BootID, 1, 128) {
		return fmt.Errorf("invalid boot ID")
	}
	if s.Host.CPUUsedPercent != nil && (math.IsNaN(*s.Host.CPUUsedPercent) || math.IsInf(*s.Host.CPUUsedPercent, 0) || *s.Host.CPUUsedPercent < 0 || *s.Host.CPUUsedPercent > 100) {
		return fmt.Errorf("invalid CPU")
	}
	if (s.Host.MemoryUsedBytes == nil) != (s.Host.MemoryTotalBytes == nil) {
		return fmt.Errorf("invalid memory pair")
	}
	if s.Host.MemoryUsedBytes != nil && *s.Host.MemoryUsedBytes > *s.Host.MemoryTotalBytes {
		return fmt.Errorf("invalid memory")
	}
	if exceedsSQLiteInteger(s.Host.UptimeSeconds) || exceedsSQLiteInteger(s.Host.MemoryUsedBytes) || exceedsSQLiteInteger(s.Host.MemoryTotalBytes) {
		return fmt.Errorf("snapshot integer exceeds storage range")
	}
	if err := validateTailscale(s.Tailscale); err != nil {
		return err
	}
	if s.Filesystems == nil || s.CollectionWarnings == nil || !validWarnings(s.CollectionWarnings) {
		return fmt.Errorf("invalid arrays")
	}
	seenIdentity := map[string]bool{}
	for _, fs := range s.Filesystems {
		if err := validateFilesystem(fs); err != nil {
			return err
		}
		kind, first := hosts.IdentityFallback, fs.Device
		if fs.FSUUID != nil {
			kind, first = hosts.IdentityUUID, *fs.FSUUID
		}
		key, err := hosts.EncodeIdentityV1(kind, first, fs.MountRoot, fs.MountPoint)
		identity := string(kind) + string(key)
		if err != nil || seenIdentity[identity] {
			return fmt.Errorf("duplicate or invalid filesystem identity")
		}
		seenIdentity[identity] = true
	}
	return nil
}

func validateSnapshot(s Snapshot) error { return ValidateSnapshot(s) }
func validText(v string, min, max int) bool {
	if len(v) < min || len(v) > max || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func validateTailscale(v Tailscale) error {
	if v.State != "connected" && v.State != "stopped" && v.State != "needs_login" && v.State != "unknown" {
		return fmt.Errorf("invalid tailscale state")
	}
	if v.Hostname != nil && !validText(*v.Hostname, 1, 253) {
		return fmt.Errorf("invalid tailscale hostname")
	}
	if v.Addresses == nil {
		return fmt.Errorf("missing tailscale addresses")
	}
	seen := map[string]bool{}
	for _, raw := range v.Addresses {
		address, err := netip.ParseAddr(raw)
		if err != nil || seen[address.String()] {
			return fmt.Errorf("invalid tailscale address")
		}
		seen[address.String()] = true
	}
	return nil
}
func validateFilesystem(v Filesystem) error {
	if !validText(v.MountPoint, 1, 4096) || !filepath.IsAbs(v.MountPoint) || filepath.Clean(v.MountPoint) != v.MountPoint {
		return fmt.Errorf("invalid mount point")
	}
	if !validText(v.MountRoot, 1, 4096) || !filepath.IsAbs(v.MountRoot) || filepath.Clean(v.MountRoot) != v.MountRoot {
		return fmt.Errorf("invalid mount root")
	}
	if !validText(v.Device, 1, 4096) || !validText(v.FSType, 1, 255) {
		return fmt.Errorf("invalid filesystem strings")
	}
	if filepath.IsAbs(v.Device) && filepath.Clean(v.Device) != v.Device {
		return fmt.Errorf("invalid filesystem device")
	}
	parts := strings.Split(v.MajorMinor, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid major minor")
	}
	for _, p := range parts {
		if p == "" || len(p) > 1 && p[0] == '0' {
			return fmt.Errorf("invalid major minor")
		}
		parsed, err := strconv.ParseUint(p, 10, 32)
		if err != nil || strconv.FormatUint(parsed, 10) != p {
			return fmt.Errorf("invalid major minor")
		}
	}
	if v.FSUUID != nil {
		if !validText(*v.FSUUID, 1, 255) || *v.FSUUID != strings.ToLower(*v.FSUUID) {
			return fmt.Errorf("invalid UUID")
		}
		for _, r := range *v.FSUUID {
			if r > 127 {
				return fmt.Errorf("invalid UUID")
			}
		}
	}
	if v.UsedBytes > v.TotalBytes || v.AvailableBytes > v.TotalBytes {
		return fmt.Errorf("invalid filesystem sizes")
	}
	if v.UsedBytes > math.MaxInt64 || v.TotalBytes > math.MaxInt64 || v.AvailableBytes > math.MaxInt64 {
		return fmt.Errorf("filesystem integer exceeds storage range")
	}
	return nil
}

func exceedsSQLiteInteger(value *uint64) bool {
	return value != nil && *value > math.MaxInt64
}
