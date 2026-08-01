package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mkrebs03/atalaya/internal/system"
)

type systemStatusRepository struct {
	hostQueryStub
	status system.Status
	err    error
}

func (repository *systemStatusRepository) SystemStatus(context.Context, string) (system.Status, error) {
	return repository.status, repository.err
}

func TestSystemStatusIsSessionProtectedAndContainsOnlyOperationalFields(t *testing.T) {
	repository := &systemStatusRepository{status: system.Status{
		Version: "0.1.0", SchemaVersion: 8, DatabaseSizeBytes: 4096,
		Collection:    system.WorkerStatus{State: "healthy", ErrorCode: ""},
		Retention:     system.WorkerStatus{State: "idle", ErrorCode: "no_targets"},
		Checks:        system.WorkerStatus{State: "unknown", ErrorCode: "no_result"},
		Notifications: system.WorkerStatus{State: "healthy", ErrorCode: ""},
	}}
	handler, cookie := authenticatedHostHandler(t, repository)

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/system/status", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/system/status", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("authenticated response = %d headers=%v", response.Code, response.Header())
	}
	if body := response.Body.String(); body == "" || !containsAll(body, "\"version\":\"0.1.0\"", "\"schema_version\":8", "\"database_size_bytes\":4096", "\"error_code\":\"no_result\"") {
		t.Fatalf("system response = %q", body)
	}

	repository.err = errors.New("secret database failure")
	failureRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/system/status", nil)
	failureRequest.AddCookie(cookie)
	failure := httptest.NewRecorder()
	handler.ServeHTTP(failure, failureRequest)
	if failure.Code != http.StatusInternalServerError || !strings.Contains(failure.Body.String(), "\"code\":\"internal_error\"") || strings.Contains(failure.Body.String(), "secret database failure") {
		t.Fatalf("sanitized failure = %d %q", failure.Code, failure.Body.String())
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
