package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/auth"
	"github.com/mkrebs03/atalaya/internal/checks"
)

type checkHTTPRepository struct {
	hostQueryStub
	item checks.Check
	run  checks.ManualRun
}

func (repository *checkHTTPRepository) List(_ context.Context, _ int, _ *checks.Cursor, _ bool) ([]checks.Check, error) {
	if repository.item.ID == "" {
		return nil, nil
	}
	return []checks.Check{repository.item}, nil
}
func (repository *checkHTTPRepository) Get(_ context.Context, id string) (checks.Check, error) {
	if id != repository.item.ID {
		return checks.Check{}, checks.ErrNotFound
	}
	return repository.item, nil
}
func (repository *checkHTTPRepository) Create(_ context.Context, item checks.Check, _ time.Time, _ checks.IDSource) (checks.Check, error) {
	repository.item = item
	repository.item.ID = "check-1"
	repository.item.Version = 1
	repository.item.ObservedState = checks.StateUnknown
	return repository.item, nil
}
func (repository *checkHTTPRepository) Update(_ context.Context, id string, version int64, item checks.Check, _ time.Time, _ checks.IDSource) error {
	if id != repository.item.ID {
		return checks.ErrNotFound
	}
	if version != repository.item.Version {
		return checks.ErrVersionConflict
	}
	repository.item = item
	repository.item.ID = id
	repository.item.Version++
	return nil
}
func (repository *checkHTTPRepository) Archive(_ context.Context, id string, version int64, _ time.Time) error {
	if id != repository.item.ID {
		return checks.ErrNotFound
	}
	if version != repository.item.Version {
		return checks.ErrVersionConflict
	}
	now := time.Now().UTC()
	repository.item.ArchivedAt = &now
	repository.item.Enabled = false
	repository.item.Version++
	return nil
}
func (repository *checkHTTPRepository) SetEnabled(_ context.Context, id string, version int64, enabled bool, _ time.Time, _ checks.IDSource) error {
	if id != repository.item.ID {
		return checks.ErrNotFound
	}
	if version != repository.item.Version {
		return checks.ErrVersionConflict
	}
	repository.item.Enabled = enabled
	repository.item.Version++
	return nil
}
func (repository *checkHTTPRepository) Restore(_ context.Context, id string, version int64, _ time.Time) error {
	if id != repository.item.ID {
		return checks.ErrNotFound
	}
	if version != repository.item.Version {
		return checks.ErrVersionConflict
	}
	repository.item.ArchivedAt = nil
	repository.item.Version++
	return nil
}
func (repository *checkHTTPRepository) ListRuns(context.Context, string, int, string) ([]checks.ManualRun, error) {
	if repository.run.ID == "" {
		return nil, nil
	}
	return []checks.ManualRun{repository.run}, nil
}
func (repository *checkHTTPRepository) GetRun(_ context.Context, checkID, runID string) (checks.ManualRun, error) {
	if checkID != repository.item.ID || runID != repository.run.ID {
		return checks.ManualRun{}, checks.ErrRunNotFound
	}
	return repository.run, nil
}
func (repository *checkHTTPRepository) EnqueueRun(_ context.Context, checkID, requestID string, now time.Time, _ checks.IDSource) (checks.ManualRun, bool, error) {
	if checkID != repository.item.ID {
		return checks.ManualRun{}, false, checks.ErrNotFound
	}
	if repository.run.ID != "" {
		if repository.run.RequestID == requestID {
			return repository.run, true, nil
		}
		return checks.ManualRun{}, false, checks.ErrRunConflict
	}
	repository.run = checks.ManualRun{ID: "run-1", CheckID: checkID, RequestID: requestID, State: "queued", RequestedAt: now, Config: repository.item.Config}
	return repository.run, false, nil
}

func TestCheckEndpointsUseCSRFAndETagForMutations(t *testing.T) {
	repository := &checkHTTPRepository{item: checks.Check{ID: "check-1", Name: "API", Config: checks.Config{Type: checks.TypeHTTP, HTTP: &checks.HTTPConfig{URL: "http://service.tailnet.ts.net/health", IntervalSeconds: 60, TimeoutSeconds: 10, ExpectedStatusMin: 200, ExpectedStatusMax: 299, MaxRedirects: 3}}, Enabled: true, ObservedState: checks.StateUnknown, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Version: 4, ConfigurationGeneration: 1}}
	handler, cookie := authenticatedHostHandler(t, repository)

	list := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/checks", nil)
	list.AddCookie(cookie)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"API"`) {
		t.Fatalf("list=%d %s", listResponse.Code, listResponse.Body.String())
	}

	csrf, err := checksCSRFToken(cookie)
	if err != nil {
		t.Fatal(err)
	}
	patch := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1:8080/api/v1/checks/check-1", strings.NewReader(`{"name":"API v2","type":"http","url":"http://service.tailnet.ts.net/health","interval_seconds":60,"timeout_seconds":10,"expected_status_min":200,"expected_status_max":299,"follow_redirects":false,"max_redirects":3}`))
	patch.AddCookie(cookie)
	patch.Header.Set("Content-Type", "application/json")
	patch.Header.Set("X-CSRF-Token", csrf)
	patch.Header.Set("If-Match", `"4"`)
	patchResponse := httptest.NewRecorder()
	handler.ServeHTTP(patchResponse, patch)
	if patchResponse.Code != http.StatusOK || patchResponse.Header().Get("ETag") != `"5"` || !strings.Contains(patchResponse.Body.String(), `"name":"API v2"`) {
		t.Fatalf("patch=%d headers=%v body=%s", patchResponse.Code, patchResponse.Header(), patchResponse.Body.String())
	}

	run := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/v1/checks/check-1/run", strings.NewReader(`{"request_id":"request-1"}`))
	run.AddCookie(cookie)
	run.Header.Set("Content-Type", "application/json")
	run.Header.Set("X-CSRF-Token", csrf)
	runResponse := httptest.NewRecorder()
	handler.ServeHTTP(runResponse, run)
	if runResponse.Code != http.StatusAccepted || !strings.Contains(runResponse.Body.String(), `"run_id":"run-1"`) {
		t.Fatalf("run=%d %s", runResponse.Code, runResponse.Body.String())
	}

	noCSRF := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/v1/checks/check-1/run", strings.NewReader(`{"request_id":"request-2"}`))
	noCSRF.AddCookie(cookie)
	noCSRF.Header.Set("Content-Type", "application/json")
	noCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(noCSRFResponse, noCSRF)
	if noCSRFResponse.Code != http.StatusForbidden || !strings.Contains(noCSRFResponse.Body.String(), `"csrf_failed"`) {
		t.Fatalf("no csrf=%d %s", noCSRFResponse.Code, noCSRFResponse.Body.String())
	}
}

func checksCSRFToken(cookie *http.Cookie) (string, error) {
	return auth.CSRFToken(cookie.Value)
}
