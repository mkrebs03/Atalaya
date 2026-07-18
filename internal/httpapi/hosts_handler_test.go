package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mkrebs03/atalaya/internal/auth"
	startupconfig "github.com/mkrebs03/atalaya/internal/config"
	"github.com/mkrebs03/atalaya/internal/hostquery"
)

type hostQueryStub struct {
	hosts       []hostquery.HostSummary
	detail      hostquery.HostDetail
	filesystems []hostquery.Filesystem
	err         error
}

func (s *hostQueryStub) ListHosts(context.Context, int, *hostquery.HostCursor) ([]hostquery.HostSummary, error) {
	return s.hosts, s.err
}
func (s *hostQueryStub) GetHost(context.Context, string) (hostquery.HostDetail, error) {
	return s.detail, s.err
}
func (s *hostQueryStub) ListFilesystems(context.Context, string, int, *hostquery.FilesystemCursor) ([]hostquery.Filesystem, error) {
	return s.filesystems, s.err
}

func authenticatedHostHandler(t *testing.T, repository hostquery.Repository) (http.Handler, *http.Cookie) {
	t.Helper()
	hash, err := auth.HashPasswordWithRandom([]byte("correct horse battery staple"), bytes.NewReader(bytes.Repeat([]byte{1}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	authRepository := &httpAuthRepository{administrator: auth.Administrator{ID: "00000000-0000-4000-8000-000000000001", Username: "admin", PasswordHash: hash, Version: 1}}
	service, err := auth.NewService(authRepository)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(fstest.MapFS{"index.html": {Data: []byte("app")}}, service, AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeDevelopment, PublicURL: "http://127.0.0.1:8080", Random: bytes.NewReader(bytes.Repeat([]byte{3}, 4096))}, repository)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"correct horse battery staple"}`))
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("login=%d %s", response.Code, response.Body.String())
	}
	return handler, response.Result().Cookies()[0]
}

func TestHostEndpointsRequireAuthenticationAndHonorNoStoreETag(t *testing.T) {
	repository := &hostQueryStub{detail: hostquery.HostDetail{HostSummary: hostquery.HostSummary{ID: "host-1", DisplayName: "Oracle", AdministrativeState: "pending", ObservedState: "unknown", Warnings: []string{}, Version: 7}, AgentURL: "https://oracle:9474"}}
	handler, cookie := authenticatedHostHandler(t, repository)
	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/hosts", nil))
	if unauthenticated.Code != http.StatusUnauthorized || !strings.Contains(unauthenticated.Body.String(), `"code":"authentication_required"`) {
		t.Fatalf("unauthenticated=%d %s", unauthenticated.Code, unauthenticated.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/hosts/host-1", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("ETag") != `"7"` {
		t.Fatalf("detail=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "display_name", "administrative_state", "observed_state", "stale", "last_attempt_at", "last_success_received_at", "freshness_deadline_at", "consecutive_failures", "cpu_used_percent", "memory_used_bytes", "memory_total_bytes", "uptime_seconds", "tailscale", "warnings", "agent_version", "protocol_version", "most_constrained_filesystem", "agent_url", "snapshot", "version"} {
		if _, ok := body[field]; !ok {
			t.Errorf("missing exact contract field %q", field)
		}
	}
	notModified := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/hosts/host-1", nil)
	notModified.AddCookie(cookie)
	notModified.Header.Set("If-None-Match", `"7"`)
	notModifiedResponse := httptest.NewRecorder()
	handler.ServeHTTP(notModifiedResponse, notModified)
	if notModifiedResponse.Code != http.StatusNotModified || notModifiedResponse.Body.Len() != 0 || notModifiedResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("304=%d headers=%v body=%q", notModifiedResponse.Code, notModifiedResponse.Header(), notModifiedResponse.Body.String())
	}
}

func TestHostDetailIfNoneMatchUsesHTTPWeakComparison(t *testing.T) {
	handler, cookie := authenticatedHostHandler(t, &hostQueryStub{detail: hostquery.HostDetail{HostSummary: hostquery.HostSummary{ID: "host-1", Warnings: []string{}, Version: 7}}})
	for _, value := range []string{`"6", "7", "8"`, ` W/"7" `, `*`} {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/hosts/host-1", nil)
		request.AddCookie(cookie)
		request.Header.Set("If-None-Match", value)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotModified || response.Body.Len() != 0 {
			t.Errorf("If-None-Match %q = %d %q", value, response.Code, response.Body.String())
		}
	}
}

func TestHostEndpointsExplicitlyRejectHEADWithoutBody(t *testing.T) {
	handler, _ := authenticatedHostHandler(t, &hostQueryStub{})
	for _, path := range []string{"/api/v1/hosts", "/api/v1/hosts/host-1", "/api/v1/hosts/host-1/filesystems"} {
		request := httptest.NewRequest(http.MethodHead, "http://127.0.0.1:8080"+path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed || response.Body.Len() != 0 || response.Header().Get("Allow") != http.MethodGet {
			t.Errorf("HEAD %s = %d allow=%q body=%q", path, response.Code, response.Header().Get("Allow"), response.Body.String())
		}
		for _, header := range []string{"Cache-Control", "Content-Security-Policy", "X-Content-Type-Options", "X-Request-ID"} {
			if response.Header().Get(header) == "" {
				t.Errorf("HEAD %s missing %s", path, header)
			}
		}
	}
}

func TestAuthenticatedHandlerWithoutHostRepositoryDoesNotRegisterHostAPI(t *testing.T) {
	service, err := auth.NewService(&httpAuthRepository{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(fstest.MapFS{"index.html": {Data: []byte("app")}}, service, AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeDevelopment, PublicURL: "http://127.0.0.1:8080", Random: bytes.NewReader(bytes.Repeat([]byte{4}, 128))})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/hosts", nil))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatalf("missing repository=%d %s", response.Code, response.Body.String())
	}
}

func TestHostListsValidatePaginationAndReturnOpaqueCursor(t *testing.T) {
	repository := &hostQueryStub{hosts: []hostquery.HostSummary{{ID: "a", DisplayName: "Alpha", Warnings: []string{}}, {ID: "b", DisplayName: "Beta", Warnings: []string{}}}, filesystems: []hostquery.Filesystem{{ID: "f1", MountPoint: "/"}, {ID: "f2", MountPoint: "/var"}}}
	handler, cookie := authenticatedHostHandler(t, repository)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/hosts?limit=1", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"next_cursor":"`) {
		t.Fatalf("list=%d %s", response.Code, response.Body.String())
	}
	invalid := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/hosts?limit=101&cursor=not-valid", nil)
	invalid.AddCookie(cookie)
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != 400 || !strings.Contains(invalidResponse.Body.String(), `"fields"`) {
		t.Fatalf("invalid=%d %s", invalidResponse.Code, invalidResponse.Body.String())
	}
	filesystem := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/hosts/a/filesystems?limit=1", nil)
	filesystem.AddCookie(cookie)
	filesystemResponse := httptest.NewRecorder()
	handler.ServeHTTP(filesystemResponse, filesystem)
	if filesystemResponse.Code != 200 || !strings.Contains(filesystemResponse.Body.String(), `"mount_point":"/"`) {
		t.Fatalf("filesystems=%d %s", filesystemResponse.Code, filesystemResponse.Body.String())
	}
}

func TestHostNotFoundIsUniform(t *testing.T) {
	handler, cookie := authenticatedHostHandler(t, &hostQueryStub{err: hostquery.ErrNotFound})
	for _, path := range []string{"/api/v1/hosts/missing", "/api/v1/hosts/missing/filesystems"} {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080"+path, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 404 || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
			t.Errorf("%s=%d %s", path, response.Code, response.Body.String())
		}
	}
}
