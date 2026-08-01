package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/hostquery"
	"github.com/mkrebs03/atalaya/internal/metrics"
)

type metricsHTTPStub struct {
	hostQueryStub
	result    metrics.Response
	queryErr  error
	settings  metrics.RetentionSettings
	updateErr error
}

func (stub *metricsHTTPStub) QueryHostMetrics(context.Context, string, metrics.Query) (metrics.Response, error) {
	return stub.result, stub.queryErr
}

func (stub *metricsHTTPStub) GetRetentionSettings(context.Context) (metrics.RetentionSettings, error) {
	return stub.settings, nil
}

func (stub *metricsHTTPStub) UpdateRetentionSettings(_ context.Context, patch metrics.SettingsPatch, version int64, now time.Time) (metrics.RetentionSettings, error) {
	if stub.updateErr != nil {
		return metrics.RetentionSettings{}, stub.updateErr
	}
	if version != stub.settings.Version {
		return metrics.RetentionSettings{}, metrics.ErrSettingsConflict
	}
	if patch.RawRetentionSeconds != nil {
		stub.settings.RawRetentionSeconds = *patch.RawRetentionSeconds
	}
	if patch.RollupRetentionSeconds != nil {
		stub.settings.RollupRetentionSeconds = *patch.RollupRetentionSeconds
	}
	stub.settings.Version++
	stub.settings.UpdatedAt = now
	return stub.settings, nil
}

func TestPatchSettingsMapsRepositoryFailuresToInternalErrors(t *testing.T) {
	stub := &metricsHTTPStub{
		hostQueryStub: hostQueryStub{detail: hostquery.HostDetail{HostSummary: hostquery.HostSummary{ID: "host-1"}}},
		settings:      metrics.RetentionSettings{RawRetentionSeconds: 172800, RollupRetentionSeconds: 7776000, Version: 3},
		updateErr:     errors.New("sqlite: database is locked"),
	}
	handler, cookie := authenticatedHostHandler(t, stub)

	csrfRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/auth/csrf", nil)
	csrfRequest.AddCookie(cookie)
	csrfResponse := httptest.NewRecorder()
	handler.ServeHTTP(csrfResponse, csrfRequest)
	var csrfBody struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(csrfResponse.Body.Bytes(), &csrfBody); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1:8080/api/v1/settings", strings.NewReader(`{"raw_retention_seconds":259200}`))
	request.AddCookie(cookie)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrfBody.CSRFToken)
	request.Header.Set("If-Match", `"3"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "database is locked") || !strings.Contains(response.Body.String(), `"internal_error"`) {
		t.Fatalf("body=%s", response.Body.String())
	}
}

func TestMetricsEndpointsExposeSeriesAndSettingsWithConcurrencyControls(t *testing.T) {
	stub := &metricsHTTPStub{
		hostQueryStub: hostQueryStub{detail: hostquery.HostDetail{HostSummary: hostquery.HostSummary{ID: "host-1", Warnings: []string{}, Version: 1}}},
		result: metrics.Response{
			From:       time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC),
			To:         time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC),
			Resolution: metrics.ResolutionRaw,
			Series:     []metrics.Series{{Key: "cpu_used_percent", Unit: "percent", Points: []metrics.Point{{Timestamp: time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC), Resolution: metrics.ResolutionRaw, Value: 12.5, Min: 12.5, Max: 12.5, SampleCount: 1}}}},
		},
		settings: metrics.RetentionSettings{RawRetentionSeconds: 172800, RollupRetentionSeconds: 7776000, UpdatedAt: time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC), Version: 3},
	}
	handler, cookie := authenticatedHostHandler(t, stub)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/hosts/host-1/metrics?from=2026-07-31T10:00:00Z&to=2026-07-31T11:00:00Z&resolution=raw", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"cpu_used_percent"`) {
		t.Fatalf("metrics=%d %s", response.Code, response.Body.String())
	}

	settingsRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/settings", nil)
	settingsRequest.AddCookie(cookie)
	settingsResponse := httptest.NewRecorder()
	handler.ServeHTTP(settingsResponse, settingsRequest)
	if settingsResponse.Code != http.StatusOK || settingsResponse.Header().Get("ETag") != `"3"` {
		t.Fatalf("settings=%d headers=%v body=%s", settingsResponse.Code, settingsResponse.Header(), settingsResponse.Body.String())
	}

	csrfRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/auth/csrf", nil)
	csrfRequest.AddCookie(cookie)
	csrfResponse := httptest.NewRecorder()
	handler.ServeHTTP(csrfResponse, csrfRequest)
	var csrfBody struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(csrfResponse.Body.Bytes(), &csrfBody); err != nil {
		t.Fatal(err)
	}
	patchRequest := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1:8080/api/v1/settings", strings.NewReader(`{"raw_retention_seconds":259200}`))
	patchRequest.AddCookie(cookie)
	patchRequest.Header.Set("Content-Type", "application/json")
	patchRequest.Header.Set("X-CSRF-Token", csrfBody.CSRFToken)
	patchRequest.Header.Set("If-Match", `"3"`)
	patchResponse := httptest.NewRecorder()
	handler.ServeHTTP(patchResponse, patchRequest)
	if patchResponse.Code != http.StatusOK || !strings.Contains(patchResponse.Body.String(), `"version":4`) {
		t.Fatalf("patch=%d %s", patchResponse.Code, patchResponse.Body.String())
	}
}

func TestMetricsEndpointMapsRawRetentionAndPointLimitErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code int
		body string
	}{
		{name: "raw", err: metrics.ErrRawNotAvailable, code: http.StatusUnprocessableEntity, body: `"raw_not_available"`},
		{name: "points", err: metrics.ErrPointLimit, code: http.StatusUnprocessableEntity, body: `"point_limit_exceeded"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &metricsHTTPStub{hostQueryStub: hostQueryStub{detail: hostquery.HostDetail{HostSummary: hostquery.HostSummary{ID: "host-1"}}}, queryErr: test.err, settings: metrics.RetentionSettings{RawRetentionSeconds: 172800, RollupRetentionSeconds: 7776000, Version: 1}}
			handler, cookie := authenticatedHostHandler(t, stub)
			request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/hosts/host-1/metrics?from=2026-07-30T10:00:00Z&to=2026-07-31T11:00:00Z", nil)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.code || !strings.Contains(response.Body.String(), test.body) {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
		})
	}
}
