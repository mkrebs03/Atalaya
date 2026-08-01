package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mkrebs03/atalaya/internal/checks"
	"github.com/mkrebs03/atalaya/internal/metrics"
)

type checkInput struct {
	Name              *string      `json:"name"`
	RelatedHostID     *string      `json:"related_host_id"`
	Type              *checks.Type `json:"type"`
	URL               *string      `json:"url"`
	Host              *string      `json:"host"`
	Port              *int         `json:"port"`
	IntervalSeconds   *int         `json:"interval_seconds"`
	TimeoutSeconds    *int         `json:"timeout_seconds"`
	ExpectedStatusMin *int         `json:"expected_status_min"`
	ExpectedStatusMax *int         `json:"expected_status_max"`
	FollowRedirects   *bool        `json:"follow_redirects"`
	MaxRedirects      *int         `json:"max_redirects"`
}

type checkJSON struct {
	ID                      string            `json:"id"`
	Name                    string            `json:"name"`
	RelatedHostID           *string           `json:"related_host_id"`
	Type                    checks.Type       `json:"type"`
	URL                     string            `json:"url,omitempty"`
	Host                    string            `json:"host,omitempty"`
	Port                    int               `json:"port,omitempty"`
	IntervalSeconds         int               `json:"interval_seconds"`
	TimeoutSeconds          int               `json:"timeout_seconds"`
	ExpectedStatusMin       int               `json:"expected_status_min,omitempty"`
	ExpectedStatusMax       int               `json:"expected_status_max,omitempty"`
	FollowRedirects         bool              `json:"follow_redirects"`
	MaxRedirects            int               `json:"max_redirects,omitempty"`
	Enabled                 bool              `json:"enabled"`
	Archived                bool              `json:"archived"`
	ArchivedAt              *time.Time        `json:"archived_at"`
	ObservedState           checks.State      `json:"observed_state"`
	LastAttemptAt           *time.Time        `json:"last_attempt_at"`
	LastResultAt            *time.Time        `json:"last_result_at"`
	FreshnessDeadlineAt     *time.Time        `json:"freshness_deadline_at"`
	Stale                   bool              `json:"stale"`
	ConsecutiveFailures     int64             `json:"consecutive_failures"`
	LastLatencyMS           *int64            `json:"last_latency_ms"`
	LastErrorCode           *checks.ErrorCode `json:"last_error_code"`
	LastHTTPStatus          *int              `json:"last_http_status"`
	CreatedAt               time.Time         `json:"created_at"`
	UpdatedAt               time.Time         `json:"updated_at"`
	ConfigurationGeneration int64             `json:"configuration_generation"`
	Version                 int64             `json:"version"`
}

type checkRunJSON struct {
	RunID           string           `json:"run_id"`
	CheckID         string           `json:"check_id"`
	RequestID       string           `json:"request_id"`
	State           string           `json:"state"`
	RequestedAt     time.Time        `json:"requested_at"`
	StartedAt       *time.Time       `json:"started_at"`
	CompletedAt     *time.Time       `json:"completed_at"`
	LeaseUntil      *time.Time       `json:"lease_until,omitempty"`
	ProcessAttempts int              `json:"process_attempts"`
	Type            checks.Type      `json:"type"`
	URL             string           `json:"url,omitempty"`
	Host            string           `json:"host,omitempty"`
	Port            int              `json:"port,omitempty"`
	IntervalSeconds int              `json:"interval_seconds"`
	TimeoutSeconds  int              `json:"timeout_seconds"`
	Result          *checkResultJSON `json:"result,omitempty"`
	FailureCode     string           `json:"failure_code,omitempty"`
}

type checkResultJSON struct {
	Status     checks.State      `json:"status"`
	LatencyMS  *int64            `json:"latency_ms"`
	ErrorCode  *checks.ErrorCode `json:"error_code"`
	HTTPStatus *int              `json:"http_status"`
}

type checkMetricsJSON struct {
	From       time.Time               `json:"from"`
	To         time.Time               `json:"to"`
	Resolution metrics.Resolution      `json:"resolution"`
	Series     []checkMetricSeriesJSON `json:"series"`
}

type checkMetricSeriesJSON struct {
	Key    string                 `json:"key"`
	Unit   string                 `json:"unit"`
	Points []checkMetricPointJSON `json:"points"`
}

type checkMetricPointJSON struct {
	Timestamp           time.Time          `json:"timestamp"`
	Resolution          metrics.Resolution `json:"resolution"`
	Value               float64            `json:"value"`
	Min                 float64            `json:"min"`
	Max                 float64            `json:"max"`
	SampleCount         int64              `json:"sample_count"`
	SuccessfulCount     *int64             `json:"successful_count,omitempty"`
	CoveredSlotCount    *int64             `json:"covered_slot_count,omitempty"`
	ExpectedSampleCount *int64             `json:"expected_sample_count,omitempty"`
	Complete            *bool              `json:"complete,omitempty"`
}

func (api *authAPI) listChecks(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	limit, cursor, ok := api.pageParameters(response, request, "include_archived")
	if !ok {
		return
	}
	includeArchived := request.URL.Query().Get("include_archived") == "true"
	var after *checks.Cursor
	if cursor != nil {
		after = &checks.Cursor{Name: cursor.First, ID: cursor.ID}
	}
	items, err := api.checks.List(request.Context(), limit+1, after, includeArchived)
	if err != nil {
		api.internal(response, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		last := items[limit-1]
		encoded := encodeCursor(pageCursor{First: normalizeNoCase(last.Name), ID: last.ID})
		next = &encoded
		items = items[:limit]
	}
	encoded := make([]checkJSON, 0, len(items))
	for _, item := range items {
		encoded = append(encoded, api.checkResponse(item))
	}
	writeJSON(response, http.StatusOK, struct {
		Items []checkJSON `json:"items"`
		Next  *string     `json:"next_cursor"`
	}{encoded, next})
}

func (api *authAPI) getCheck(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	item, err := api.checks.Get(request.Context(), request.PathValue("id"))
	if errors.Is(err, checks.ErrNotFound) {
		api.notFound(response, request)
		return
	}
	if err != nil {
		api.internal(response, request, err)
		return
	}
	etag := fmt.Sprintf("\"%d\"", item.Version)
	response.Header().Set("ETag", etag)
	if ifNoneMatch(request.Header.Values("If-None-Match"), etag) {
		response.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(response, http.StatusOK, api.checkResponse(item))
}

func (api *authAPI) createCheck(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	var input checkInput
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	item, err := api.checkFromInput(request.Context(), input, nil)
	if err != nil {
		api.writeCheckError(response, request, err)
		return
	}
	created, err := api.checks.Create(request.Context(), item, api.config.Clock().UTC(), checks.NewReaderIDSource(api.config.Random))
	if err != nil {
		api.writeCheckError(response, request, err)
		return
	}
	response.Header().Set("ETag", fmt.Sprintf("\"%d\"", created.Version))
	writeJSON(response, http.StatusCreated, api.checkResponse(created))
}

func (api *authAPI) patchCheck(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	version, ok := requireIfMatch(response, request)
	if !ok {
		return
	}
	var input checkInput
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	current, err := api.checks.Get(request.Context(), request.PathValue("id"))
	if err != nil {
		api.writeCheckError(response, request, err)
		return
	}
	item, err := api.checkFromInput(request.Context(), input, &current)
	if err != nil {
		api.writeCheckError(response, request, err)
		return
	}
	if err := api.checks.Update(request.Context(), current.ID, version, item, api.config.Clock().UTC(), checks.NewReaderIDSource(api.config.Random)); err != nil {
		api.writeCheckError(response, request, err)
		return
	}
	api.writeCheck(request, response, current.ID)
}

func (api *authAPI) archiveCheck(response http.ResponseWriter, request *http.Request) {
	api.checkAction(response, request, func(id string, version int64) error {
		return api.checks.Archive(request.Context(), id, version, api.config.Clock().UTC())
	}, true)
}

func (api *authAPI) enableCheck(response http.ResponseWriter, request *http.Request) {
	api.checkAction(response, request, func(id string, version int64) error {
		return api.checks.SetEnabled(request.Context(), id, version, true, api.config.Clock().UTC(), checks.NewReaderIDSource(api.config.Random))
	}, false)
}

func (api *authAPI) disableCheck(response http.ResponseWriter, request *http.Request) {
	api.checkAction(response, request, func(id string, version int64) error {
		return api.checks.SetEnabled(request.Context(), id, version, false, api.config.Clock().UTC(), checks.NewReaderIDSource(api.config.Random))
	}, false)
}

func (api *authAPI) restoreCheck(response http.ResponseWriter, request *http.Request) {
	api.checkAction(response, request, func(id string, version int64) error {
		return api.checks.Restore(request.Context(), id, version, api.config.Clock().UTC())
	}, true)
}

func (api *authAPI) checkAction(response http.ResponseWriter, request *http.Request, action func(string, int64) error, noContent bool) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	version, ok := requireIfMatch(response, request)
	if !ok {
		return
	}
	if err := action(request.PathValue("id"), version); err != nil {
		api.writeCheckError(response, request, err)
		return
	}
	if noContent {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	api.writeCheck(request, response, request.PathValue("id"))
}

func (api *authAPI) writeCheck(request *http.Request, response http.ResponseWriter, id string) {
	item, err := api.checks.Get(request.Context(), id)
	if err != nil {
		api.writeCheckError(response, request, err)
		return
	}
	response.Header().Set("ETag", fmt.Sprintf("\"%d\"", item.Version))
	writeJSON(response, http.StatusOK, api.checkResponse(item))
}

func (api *authAPI) runCheck(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	var input struct {
		RequestID string `json:"request_id"`
	}
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	run, _, err := api.checks.EnqueueRun(request.Context(), request.PathValue("id"), input.RequestID, api.config.Clock().UTC(), checks.NewReaderIDSource(api.config.Random))
	if err != nil {
		api.writeCheckError(response, request, err)
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]string{"run_id": run.ID, "status": run.State})
}

func (api *authAPI) listCheckRuns(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	limit, cursor, ok := api.pageParameters(response, request, "from", "to")
	if !ok {
		return
	}
	from, to, ok := parseOptionalRange(response, request)
	if !ok {
		return
	}
	opaqueCursor := ""
	if cursor != nil {
		opaqueCursor = cursor.First + "\x00" + cursor.ID
	}
	var items []checks.ManualRun
	var err error
	if ranged, available := api.checks.(interface {
		ListRunsBetween(context.Context, string, int, string, *time.Time, *time.Time) ([]checks.ManualRun, error)
	}); available {
		items, err = ranged.ListRunsBetween(request.Context(), request.PathValue("id"), limit+1, opaqueCursor, from, to)
	} else {
		items, err = api.checks.ListRuns(request.Context(), request.PathValue("id"), limit+1, opaqueCursor)
	}
	if err != nil {
		api.writeCheckError(response, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		last := items[limit-1]
		encoded := encodeCursor(pageCursor{First: last.RequestedAt.UTC().Format(time.RFC3339Nano), ID: last.ID})
		next = &encoded
		items = items[:limit]
	}
	encoded := make([]checkRunJSON, 0, len(items))
	for _, item := range items {
		encoded = append(encoded, api.runResponse(item))
	}
	writeJSON(response, http.StatusOK, struct {
		Items []checkRunJSON `json:"items"`
		Next  *string        `json:"next_cursor"`
	}{encoded, next})
}

func (api *authAPI) getCheckRun(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	run, err := api.checks.GetRun(request.Context(), request.PathValue("id"), request.PathValue("run_id"))
	if err != nil {
		api.writeCheckError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, api.runResponse(run))
}

func (api *authAPI) getCheckMetrics(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	values := request.URL.Query()
	fields := map[string]string{}
	from, fromOK := parseSingleMetricTime(values["from"])
	to, toOK := parseSingleMetricTime(values["to"])
	if !fromOK {
		fields["from"] = "must be a single RFC3339 timestamp"
	}
	if !toOK {
		fields["to"] = "must be a single RFC3339 timestamp"
	}
	resolution := metrics.ResolutionAuto
	if raw, exists := values["resolution"]; exists {
		if len(raw) != 1 {
			fields["resolution"] = "must be auto, raw or hourly"
		} else {
			resolution = metrics.Resolution(raw[0])
			if resolution != metrics.ResolutionAuto && resolution != metrics.ResolutionRaw && resolution != metrics.ResolutionHourly {
				fields["resolution"] = "must be auto, raw or hourly"
			}
		}
	}
	for key := range values {
		if key != "from" && key != "to" && key != "resolution" {
			fields[key] = "is not supported"
		}
	}
	if fromOK && toOK && !from.Before(to) {
		fields["to"] = "must be after from"
	}
	if len(fields) > 0 {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", fields)
		return
	}
	query := metrics.Query{From: from, To: to, Resolution: resolution}
	if err := metrics.ValidateQuery(query); err != nil {
		api.writeError(response, request, http.StatusUnprocessableEntity, "validation_failed", "Request is invalid", map[string]string{"range": err.Error()})
		return
	}
	result, err := api.checkMetrics.QueryCheckMetrics(request.Context(), request.PathValue("id"), query)
	if errors.Is(err, metrics.ErrCheckNotFound) {
		api.notFound(response, request)
		return
	}
	if errors.Is(err, metrics.ErrRawNotAvailable) {
		fields := map[string]string{}
		if api.settings != nil {
			if current, settingsErr := api.settings.GetSettings(request.Context()); settingsErr == nil {
				fields["earliest_available_at"] = api.config.Clock().UTC().Add(-time.Duration(current.RawRetentionSeconds) * time.Second).Format(time.RFC3339Nano)
			}
		}
		api.writeError(response, request, http.StatusUnprocessableEntity, "raw_not_available", "Raw metrics are not available for the requested range", fields)
		return
	}
	if errors.Is(err, metrics.ErrPointLimit) {
		api.writeError(response, request, http.StatusUnprocessableEntity, "point_limit_exceeded", "The requested range contains too many points", nil)
		return
	}
	if err != nil {
		api.internal(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, checkMetricsResponse(result))
}

func checkMetricsResponse(result metrics.Response) checkMetricsJSON {
	response := checkMetricsJSON{From: result.From, To: result.To, Resolution: result.Resolution, Series: make([]checkMetricSeriesJSON, 0, len(result.Series))}
	for _, series := range result.Series {
		encoded := checkMetricSeriesJSON{Key: series.Key, Unit: series.Unit, Points: make([]checkMetricPointJSON, 0, len(series.Points))}
		for _, point := range series.Points {
			encodedPoint := checkMetricPointJSON{Timestamp: point.Timestamp, Resolution: point.Resolution, Value: point.Value, Min: point.Min, Max: point.Max, SampleCount: point.SampleCount}
			if series.Key == "availability" {
				complete := point.Complete
				encodedPoint.SuccessfulCount = point.SuccessfulCount
				encodedPoint.CoveredSlotCount = point.CoveredSlotCount
				encodedPoint.ExpectedSampleCount = point.ExpectedSampleCount
				encodedPoint.Complete = &complete
			}
			encoded.Points = append(encoded.Points, encodedPoint)
		}
		response.Series = append(response.Series, encoded)
	}
	return response
}

func parseOptionalRange(response http.ResponseWriter, request *http.Request) (*time.Time, *time.Time, bool) {
	values := request.URL.Query()
	fields := map[string]string{}
	var from, to *time.Time
	for key, target := range map[string]**time.Time{"from": &from, "to": &to} {
		if raw, exists := values[key]; exists {
			if len(raw) != 1 {
				fields[key] = "must be a single RFC3339 timestamp"
			} else if parsed, ok := parseMetricTime(raw[0]); !ok {
				fields[key] = "must be a single RFC3339 timestamp"
			} else {
				*target = &parsed
			}
		}
	}
	if from != nil && to != nil && !from.Before(*to) {
		fields["to"] = "must be after from"
	}
	for key := range values {
		if key != "from" && key != "to" && key != "cursor" && key != "limit" {
			fields[key] = "is not supported"
		}
	}
	if len(fields) > 0 {
		response.Header().Set("Content-Type", "application/json")
		// The caller owns the API error shape; this helper only handles query validation.
		body := map[string]any{"error": map[string]any{"code": "validation_failed", "message": "Request is invalid", "fields": fields, "request_id": requestID(request.Context())}}
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(body)
		return nil, nil, false
	}
	return from, to, true
}

func (api *authAPI) checkFromInput(ctx context.Context, input checkInput, current *checks.Check) (checks.Check, error) {
	item := checks.Check{}
	if current != nil {
		item = *current
	}
	if input.Name != nil {
		item.Name = *input.Name
	}
	if current == nil && input.Name == nil {
		return checks.Check{}, checks.ErrInvalid
	}
	if input.RelatedHostID != nil {
		item.RelatedHostID = input.RelatedHostID
	}
	typeName := input.Type
	if typeName == nil && current != nil {
		typeName = &item.Config.Type
	}
	if typeName == nil {
		return checks.Check{}, checks.ErrInvalid
	}
	interval, timeout := checks.DefaultInterval/time.Second, checks.DefaultTimeout/time.Second
	if api.settings != nil {
		defaults, err := api.settings.GetSettings(ctx)
		if err != nil {
			return checks.Check{}, err
		}
		interval = time.Duration(defaults.CheckIntervalSeconds) * time.Second
		timeout = time.Duration(defaults.CheckTimeoutSeconds) * time.Second
	}
	if current != nil {
		interval, timeout = item.Config.Interval(), item.Config.Timeout()
	}
	if input.IntervalSeconds != nil {
		interval = time.Duration(*input.IntervalSeconds) * time.Second
	}
	if input.TimeoutSeconds != nil {
		timeout = time.Duration(*input.TimeoutSeconds) * time.Second
	}
	switch *typeName {
	case checks.TypeHTTP:
		if input.Host != nil || input.Port != nil {
			return checks.Check{}, checks.ErrInvalid
		}
		config := checks.HTTPConfig{IntervalSeconds: int(interval / time.Second), TimeoutSeconds: int(timeout / time.Second), ExpectedStatusMin: 200, ExpectedStatusMax: 299, MaxRedirects: 3}
		if current != nil && item.Config.HTTP != nil {
			config = *item.Config.HTTP
		}
		if input.URL != nil {
			config.URL = *input.URL
		}
		if input.ExpectedStatusMin != nil {
			config.ExpectedStatusMin = *input.ExpectedStatusMin
		}
		if input.ExpectedStatusMax != nil {
			config.ExpectedStatusMax = *input.ExpectedStatusMax
		}
		if input.FollowRedirects != nil {
			config.FollowRedirects = *input.FollowRedirects
		}
		if input.MaxRedirects != nil {
			config.MaxRedirects = *input.MaxRedirects
		}
		config.IntervalSeconds, config.TimeoutSeconds = int(interval/time.Second), int(timeout/time.Second)
		item.Config = checks.Config{Type: checks.TypeHTTP, HTTP: &config}
	case checks.TypeTCP:
		if input.URL != nil || input.ExpectedStatusMin != nil || input.ExpectedStatusMax != nil || input.FollowRedirects != nil || input.MaxRedirects != nil {
			return checks.Check{}, checks.ErrInvalid
		}
		config := checks.TCPConfig{IntervalSeconds: int(interval / time.Second), TimeoutSeconds: int(timeout / time.Second)}
		if current != nil && item.Config.TCP != nil {
			config = *item.Config.TCP
		}
		if input.Host != nil {
			config.Host = *input.Host
		}
		if input.Port != nil {
			config.Port = *input.Port
		}
		config.IntervalSeconds, config.TimeoutSeconds = int(interval/time.Second), int(timeout/time.Second)
		item.Config = checks.Config{Type: checks.TypeTCP, TCP: &config}
	default:
		return checks.Check{}, checks.ErrInvalid
	}
	return item, nil
}

func (api *authAPI) checkResponse(item checks.Check) checkJSON {
	result := checkJSON{ID: item.ID, Name: item.Name, RelatedHostID: item.RelatedHostID, Enabled: item.Enabled, Archived: item.ArchivedAt != nil, ArchivedAt: item.ArchivedAt, ObservedState: item.ObservedState, LastAttemptAt: item.LastAttemptAt, LastResultAt: item.LastResultAt, FreshnessDeadlineAt: item.FreshnessDeadlineAt, ConsecutiveFailures: item.ConsecutiveFailures, LastLatencyMS: item.LastLatencyMS, LastErrorCode: item.LastErrorCode, LastHTTPStatus: item.LastHTTPStatus, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, ConfigurationGeneration: item.ConfigurationGeneration, Version: item.Version}
	result.Stale = item.FreshnessDeadlineAt != nil && !item.FreshnessDeadlineAt.After(api.config.Clock())
	result.Type = item.Config.Type
	if item.Config.HTTP != nil {
		result.URL = item.Config.HTTP.URL
		result.IntervalSeconds, result.TimeoutSeconds = item.Config.HTTP.IntervalSeconds, item.Config.HTTP.TimeoutSeconds
		result.ExpectedStatusMin, result.ExpectedStatusMax = item.Config.HTTP.ExpectedStatusMin, item.Config.HTTP.ExpectedStatusMax
		result.FollowRedirects, result.MaxRedirects = item.Config.HTTP.FollowRedirects, item.Config.HTTP.MaxRedirects
	} else if item.Config.TCP != nil {
		result.Host, result.Port = item.Config.TCP.Host, item.Config.TCP.Port
		result.IntervalSeconds, result.TimeoutSeconds = item.Config.TCP.IntervalSeconds, item.Config.TCP.TimeoutSeconds
	}
	return result
}

func (api *authAPI) runResponse(run checks.ManualRun) checkRunJSON {
	result := checkRunJSON{RunID: run.ID, CheckID: run.CheckID, RequestID: run.RequestID, State: run.State, RequestedAt: run.RequestedAt, StartedAt: run.StartedAt, CompletedAt: run.CompletedAt, LeaseUntil: run.LeaseUntil, ProcessAttempts: run.ProcessAttempts, Type: run.Config.Type, FailureCode: run.FailureCode}
	if run.Config.HTTP != nil {
		result.URL = run.Config.HTTP.URL
		result.IntervalSeconds, result.TimeoutSeconds = run.Config.HTTP.IntervalSeconds, run.Config.HTTP.TimeoutSeconds
	} else if run.Config.TCP != nil {
		result.Host, result.Port = run.Config.TCP.Host, run.Config.TCP.Port
		result.IntervalSeconds, result.TimeoutSeconds = run.Config.TCP.IntervalSeconds, run.Config.TCP.TimeoutSeconds
	}
	if run.Result != nil {
		result.Result = &checkResultJSON{Status: run.Result.Status, LatencyMS: run.Result.LatencyMS, ErrorCode: run.Result.ErrorCode, HTTPStatus: run.Result.HTTPStatus}
	}
	return result
}

func (api *authAPI) writeCheckError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, checks.ErrNotFound), errors.Is(err, checks.ErrRunNotFound):
		api.notFound(response, request)
	case errors.Is(err, checks.ErrVersionConflict):
		writeBareError(response, request, http.StatusPreconditionFailed, "version_conflict", "Resource version does not match")
	case errors.Is(err, checks.ErrArchived):
		api.writeError(response, request, http.StatusConflict, "check_archived", "Check is archived", nil)
	case errors.Is(err, checks.ErrRunConflict):
		api.writeError(response, request, http.StatusConflict, "run_conflict", "A run with this request ID already exists", nil)
	case errors.Is(err, checks.ErrInvalid), errors.Is(err, checks.ErrInvalidSettings):
		api.writeError(response, request, http.StatusUnprocessableEntity, "validation_failed", "Check configuration is invalid", nil)
	default:
		api.internal(response, request, err)
	}
}
