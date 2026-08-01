package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mkrebs03/atalaya/internal/metrics"
	settingsdomain "github.com/mkrebs03/atalaya/internal/settings"
)

type settingsJSON struct {
	RawRetentionSeconds    int64     `json:"raw_retention_seconds"`
	RollupRetentionSeconds int64     `json:"rollup_retention_seconds"`
	CheckIntervalSeconds   int       `json:"check_interval_seconds"`
	CheckTimeoutSeconds    int       `json:"check_timeout_seconds"`
	AllowedPrivateCIDRs    []string  `json:"allowed_private_cidrs"`
	UpdatedAt              time.Time `json:"updated_at"`
	Version                int64     `json:"version"`
}

func (api *authAPI) getHostMetrics(response http.ResponseWriter, request *http.Request) {
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
	result, err := api.metrics.QueryHostMetrics(request.Context(), request.PathValue("id"), query)
	if errors.Is(err, metrics.ErrNotFound) {
		api.notFound(response, request)
		return
	}
	if errors.Is(err, metrics.ErrRawNotAvailable) {
		settings, settingsErr := api.metrics.GetRetentionSettings(request.Context())
		fields := map[string]string{}
		if settingsErr == nil {
			fields["earliest_available_at"] = api.config.Clock().UTC().Add(-time.Duration(settings.RawRetentionSeconds) * time.Second).Format(time.RFC3339Nano)
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
	writeJSON(response, http.StatusOK, result)
}

func (api *authAPI) getSettings(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	if api.settings != nil {
		settings, err := api.settings.GetSettings(request.Context())
		if err != nil {
			api.internal(response, request, err)
			return
		}
		response.Header().Set("ETag", fmt.Sprintf("\"%d\"", settings.Version))
		writeJSON(response, http.StatusOK, settingsResponse(settings))
		return
	}
	settings, err := api.metrics.GetRetentionSettings(request.Context())
	if err != nil {
		api.internal(response, request, err)
		return
	}
	response.Header().Set("ETag", fmt.Sprintf("\"%d\"", settings.Version))
	writeJSON(response, http.StatusOK, settings)
}

func (api *authAPI) patchSettings(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	version, ok := requireIfMatch(response, request)
	if !ok {
		return
	}
	if api.settings != nil {
		var patch settingsdomain.Patch
		if err := decodeJSON(response, request, &patch); err != nil || patch.RawRetentionSeconds == nil && patch.RollupRetentionSeconds == nil && patch.CheckIntervalSeconds == nil && patch.CheckTimeoutSeconds == nil && patch.AllowedPrivateCIDRs == nil {
			api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
			return
		}
		settings, err := api.settings.UpdateSettings(request.Context(), patch, version, api.config.Clock().UTC())
		if errors.Is(err, settingsdomain.ErrVersionConflict) {
			writeBareError(response, request, http.StatusPreconditionFailed, "version_conflict", "Resource version does not match")
			return
		}
		if errors.Is(err, settingsdomain.ErrInvalid) {
			api.writeError(response, request, http.StatusUnprocessableEntity, "validation_failed", "Settings are invalid", nil)
			return
		}
		if err != nil {
			api.internal(response, request, err)
			return
		}
		response.Header().Set("ETag", fmt.Sprintf("\"%d\"", settings.Version))
		writeJSON(response, http.StatusOK, settingsResponse(settings))
		return
	}
	var patch metrics.SettingsPatch
	if err := decodeJSON(response, request, &patch); err != nil || patch.RawRetentionSeconds == nil && patch.RollupRetentionSeconds == nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	settings, err := api.metrics.UpdateRetentionSettings(request.Context(), patch, version, api.config.Clock().UTC())
	if errors.Is(err, metrics.ErrSettingsConflict) {
		writeBareError(response, request, http.StatusPreconditionFailed, "version_conflict", "Resource version does not match")
		return
	}
	if err != nil {
		var validationErr *metrics.ValidationError
		if errors.As(err, &validationErr) {
			api.writeError(response, request, http.StatusUnprocessableEntity, "validation_failed", "Retention settings are invalid", map[string]string{"retention": validationErr.Error()})
			return
		}
		api.internal(response, request, err)
		return
	}
	response.Header().Set("ETag", fmt.Sprintf("\"%d\"", settings.Version))
	writeJSON(response, http.StatusOK, settings)
}

func settingsResponse(value settingsdomain.Settings) settingsJSON {
	return settingsJSON{RawRetentionSeconds: value.RawRetentionSeconds, RollupRetentionSeconds: value.RollupRetentionSeconds, CheckIntervalSeconds: value.CheckIntervalSeconds, CheckTimeoutSeconds: value.CheckTimeoutSeconds, AllowedPrivateCIDRs: value.AllowedPrivateCIDRs, UpdatedAt: value.UpdatedAt, Version: value.Version}
}

func parseMetricTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed.UTC(), err == nil
}

func parseSingleMetricTime(values []string) (time.Time, bool) {
	if len(values) != 1 {
		return time.Time{}, false
	}
	return parseMetricTime(values[0])
}
