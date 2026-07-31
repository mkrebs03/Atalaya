package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/auth"
	"github.com/mkrebs03/atalaya/internal/hostquery"
	"github.com/mkrebs03/atalaya/internal/hosts"
)

type managementHTTPStub struct {
	hostQueryStub
	enrollment hosts.EnrollmentResponse
	token      hosts.EnrollmentToken
	secret     string
	archived   bool
}

func (stub *managementHTTPStub) CreateEnrollmentToken(context.Context, string, string, time.Time, io.Reader) (hosts.EnrollmentToken, string, error) {
	return stub.token, stub.secret, nil
}
func (*managementHTTPStub) ListEnrollmentTokens(context.Context, int, string) ([]hosts.EnrollmentToken, error) {
	return nil, nil
}
func (*managementHTTPStub) RevokeEnrollmentToken(context.Context, string, int64, time.Time) error {
	return nil
}
func (stub *managementHTTPStub) EnrollAgent(context.Context, hosts.EnrollmentRequest, time.Time, io.Reader) (hosts.EnrollmentResponse, error) {
	return stub.enrollment, nil
}
func (*managementHTTPStub) PatchHost(context.Context, string, int64, hosts.HostPatch, time.Time, io.Reader) error {
	return nil
}
func (*managementHTTPStub) SetHostAdministrativeState(context.Context, string, int64, string, time.Time, io.Reader) error {
	return nil
}
func (*managementHTTPStub) RestoreHost(context.Context, string, int64, time.Time) error { return nil }
func (*managementHTTPStub) SelectFilesystem(context.Context, string, string, int64, bool, time.Time) error {
	return nil
}
func (stub *managementHTTPStub) ListHostsIncludingArchived(context.Context, int, *hostquery.HostCursor) ([]hostquery.HostSummary, error) {
	if stub.archived {
		return []hostquery.HostSummary{{ID: "archived", Archived: true}}, nil
	}
	return nil, nil
}

func TestManagementEndpointsKeepEnrollmentUnauthenticatedAndMutationsCSRFProtected(t *testing.T) {
	secret := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	stub := &managementHTTPStub{
		hostQueryStub: hostQueryStub{detail: hostquery.HostDetail{HostSummary: hostquery.HostSummary{ID: "host-1", Version: 4}}},
		enrollment:    hosts.EnrollmentResponse{HostID: "host-1", AgentCredential: bytes.Repeat([]byte{8}, 32), PollIntervalSeconds: 60},
		token:         hosts.EnrollmentToken{ID: "token-1", CreatedAt: time.Unix(1, 0), ExpiresAt: time.Unix(901, 0), Version: 1},
		secret:        secret,
	}
	handler, cookie := authenticatedHostHandler(t, stub)
	enrollment := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/v1/agent-enrollments", strings.NewReader(`{"enrollment_token":"token","request_id":"request","display_name":"Remote","agent_url":"https://remote.example.ts.net:9474","agent_version":"dev","protocol_version":1}`))
	enrollment.Header.Set("Content-Type", "application/json")
	enrollmentResponse := httptest.NewRecorder()
	handler.ServeHTTP(enrollmentResponse, enrollment)
	if enrollmentResponse.Code != http.StatusOK || !strings.Contains(enrollmentResponse.Body.String(), "agent_credential") || strings.Contains(enrollmentResponse.Body.String(), string(bytes.Repeat([]byte{8}, 32))) {
		t.Fatalf("enrollment=%d %s", enrollmentResponse.Code, enrollmentResponse.Body.String())
	}

	create := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/v1/enrollment-tokens", strings.NewReader(`{"label":"remote"}`))
	create.AddCookie(cookie)
	create.Header.Set("Origin", "http://127.0.0.1:8080")
	create.Header.Set("Content-Type", "application/json")
	withoutCSRF := httptest.NewRecorder()
	handler.ServeHTTP(withoutCSRF, create)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("without CSRF=%d %s", withoutCSRF.Code, withoutCSRF.Body.String())
	}
	csrf, err := auth.CSRFToken(cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	create = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/v1/enrollment-tokens", strings.NewReader(`{"label":"remote"}`))
	create.AddCookie(cookie)
	create.Header.Set("Origin", "http://127.0.0.1:8080")
	create.Header.Set("Content-Type", "application/json")
	create.Header.Set("X-CSRF-Token", csrf)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated || created.Header().Get("Cache-Control") != "no-store" || !strings.Contains(created.Body.String(), `"token":"`+secret+`"`) {
		t.Fatalf("created=%d headers=%v body=%s", created.Code, created.Header(), created.Body.String())
	}

	patch := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1:8080/api/v1/hosts/host-1", strings.NewReader(`{"interval_seconds":75}`))
	patch.AddCookie(cookie)
	patch.Header.Set("Origin", "http://127.0.0.1:8080")
	patch.Header.Set("X-CSRF-Token", csrf)
	patch.Header.Set("If-Match", `"4"`)
	patch.Header.Set("Content-Type", "application/json")
	patched := httptest.NewRecorder()
	handler.ServeHTTP(patched, patch)
	if patched.Code != http.StatusOK || !strings.Contains(patched.Body.String(), `"id":"host-1"`) {
		t.Fatalf("patch=%d %s", patched.Code, patched.Body.String())
	}

	missingVersion := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1:8080/api/v1/hosts/host-1", strings.NewReader(`{"display_name":"changed"}`))
	missingVersion.AddCookie(cookie)
	missingVersion.Header.Set("Origin", "http://127.0.0.1:8080")
	missingVersion.Header.Set("X-CSRF-Token", csrf)
	missingVersion.Header.Set("Content-Type", "application/json")
	versionResponse := httptest.NewRecorder()
	handler.ServeHTTP(versionResponse, missingVersion)
	if versionResponse.Code != http.StatusPreconditionRequired || !strings.Contains(versionResponse.Body.String(), `"code":"version_required"`) {
		t.Fatalf("missing version=%d %s", versionResponse.Code, versionResponse.Body.String())
	}
}

func TestHostListCanExplicitlyIncludeArchivedResources(t *testing.T) {
	stub := &managementHTTPStub{hostQueryStub: hostQueryStub{}, archived: true}
	handler, cookie := authenticatedHostHandler(t, stub)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/hosts?include_archived=true", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"archived":true`) {
		t.Fatalf("list=%d %s", response.Code, response.Body.String())
	}
	var body struct {
		Items []hostquery.HostSummary `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Items) != 1 || !body.Items[0].Archived {
		t.Fatalf("body=%s err=%v", response.Body.String(), err)
	}
}
