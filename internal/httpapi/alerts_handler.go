package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mkrebs03/atalaya/internal/alerts"
)

type alertRuleInput struct {
	Name                *string           `json:"name"`
	Type                *alerts.RuleType  `json:"type"`
	Severity            *alerts.Severity  `json:"severity"`
	HostID              *string           `json:"host_id"`
	CheckID             *string           `json:"check_id"`
	FilesystemID        *string           `json:"filesystem_id"`
	Mode                *alerts.CheckMode `json:"mode"`
	ForSeconds          *int              `json:"for_seconds"`
	ConsecutiveFailures *int              `json:"consecutive_failures"`
	Metric              *alerts.Metric    `json:"metric"`
	Operator            *alerts.Operator  `json:"operator"`
	Threshold           *float64          `json:"threshold"`
	ChannelIDs          *[]string         `json:"channel_ids"`
}

func (api *authAPI) listAlertRules(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	limit, cursor, ok := api.pageParameters(response, request, "include_archived")
	if !ok {
		return
	}
	includeArchived := request.URL.Query().Get("include_archived") == "true"
	var after *alerts.RuleCursor
	if cursor != nil {
		after = &alerts.RuleCursor{Name: cursor.First, ID: cursor.ID}
	}
	items, err := api.alerts.ListAlertRules(request.Context(), limit+1, after, includeArchived)
	if err != nil {
		api.writeAlertError(response, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		last := items[limit-1]
		encoded := encodeCursor(pageCursor{First: normalizeNoCase(last.Name), ID: last.ID})
		next = &encoded
		items = items[:limit]
	}
	encoded := make([]map[string]any, 0, len(items))
	for _, item := range items {
		encoded = append(encoded, alertRuleResponse(item))
	}
	writeJSON(response, http.StatusOK, struct {
		Items []map[string]any `json:"items"`
		Next  *string          `json:"next_cursor"`
	}{encoded, next})
}

func (api *authAPI) getAlertRule(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	item, err := api.alerts.GetAlertRule(request.Context(), request.PathValue("id"))
	if err != nil {
		api.writeAlertError(response, request, err)
		return
	}
	etag := fmt.Sprintf("\"%d\"", item.Version)
	response.Header().Set("ETag", etag)
	if ifNoneMatch(request.Header.Values("If-None-Match"), etag) {
		response.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(response, http.StatusOK, alertRuleResponse(item))
}

func (api *authAPI) createAlertRule(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	var input alertRuleInput
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	rule := alertRuleFromInput(input, nil)
	rule.Enabled = true
	created, err := api.alerts.CreateAlertRule(request.Context(), rule, api.config.Clock().UTC(), alerts.NewReaderIDSource(api.config.Random))
	if err != nil {
		api.writeAlertError(response, request, err)
		return
	}
	response.Header().Set("ETag", fmt.Sprintf("\"%d\"", created.Version))
	writeJSON(response, http.StatusCreated, alertRuleResponse(created))
}

func (api *authAPI) patchAlertRule(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	version, ok := requireIfMatch(response, request)
	if !ok {
		return
	}
	var input alertRuleInput
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	current, err := api.alerts.GetAlertRule(request.Context(), request.PathValue("id"))
	if err != nil {
		api.writeAlertError(response, request, err)
		return
	}
	next := alertRuleFromInput(input, &current)
	if err := api.alerts.UpdateAlertRule(request.Context(), current.ID, version, next, api.config.Clock().UTC()); err != nil {
		api.writeAlertError(response, request, err)
		return
	}
	api.writeUpdatedAlertRule(response, request, current.ID)
}

func (api *authAPI) archiveAlertRule(response http.ResponseWriter, request *http.Request) {
	api.alertRuleAction(response, request, func(id string, version int64) error {
		return api.alerts.ArchiveAlertRule(request.Context(), id, version, api.config.Clock().UTC())
	}, true)
}

func (api *authAPI) enableAlertRule(response http.ResponseWriter, request *http.Request) {
	api.alertRuleAction(response, request, func(id string, version int64) error {
		return api.alerts.SetAlertRuleEnabled(request.Context(), id, version, true, api.config.Clock().UTC())
	}, false)
}

func (api *authAPI) disableAlertRule(response http.ResponseWriter, request *http.Request) {
	api.alertRuleAction(response, request, func(id string, version int64) error {
		return api.alerts.SetAlertRuleEnabled(request.Context(), id, version, false, api.config.Clock().UTC())
	}, false)
}

func (api *authAPI) restoreAlertRule(response http.ResponseWriter, request *http.Request) {
	api.alertRuleAction(response, request, func(id string, version int64) error {
		return api.alerts.RestoreAlertRule(request.Context(), id, version, api.config.Clock().UTC())
	}, false)
}

func (api *authAPI) alertRuleAction(response http.ResponseWriter, request *http.Request, action func(string, int64) error, noContent bool) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	version, ok := requireIfMatch(response, request)
	if !ok {
		return
	}
	if err := action(request.PathValue("id"), version); err != nil {
		api.writeAlertError(response, request, err)
		return
	}
	if noContent {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	api.writeUpdatedAlertRule(response, request, request.PathValue("id"))
}

func (api *authAPI) writeUpdatedAlertRule(response http.ResponseWriter, request *http.Request, id string) {
	item, err := api.alerts.GetAlertRule(request.Context(), id)
	if err != nil {
		api.writeAlertError(response, request, err)
		return
	}
	response.Header().Set("ETag", fmt.Sprintf("\"%d\"", item.Version))
	writeJSON(response, http.StatusOK, alertRuleResponse(item))
}

func (api *authAPI) listIncidents(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	limit, cursor, ok := api.pageParameters(response, request, "state", "severity")
	if !ok {
		return
	}
	state := request.URL.Query().Get("state")
	severity := request.URL.Query().Get("severity")
	fields := map[string]string{}
	if state != "" && state != "open" && state != "resolved" {
		fields["state"] = "must be open or resolved"
	}
	if severity != "" && severity != string(alerts.SeverityWarning) && severity != string(alerts.SeverityCritical) {
		fields["severity"] = "must be warning or critical"
	}
	if len(fields) != 0 {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", fields)
		return
	}
	var after *alerts.IncidentCursor
	if cursor != nil {
		firedAt, err := time.Parse(time.RFC3339Nano, cursor.First)
		if err != nil {
			api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", map[string]string{"cursor": "must be a valid opaque cursor"})
			return
		}
		after = &alerts.IncidentCursor{FiredAt: firedAt, ID: cursor.ID}
	}
	items, err := api.alerts.ListIncidents(request.Context(), state, severity, limit+1, after)
	if err != nil {
		api.writeAlertError(response, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		last := items[limit-1]
		encoded := encodeCursor(pageCursor{First: last.FiredAt.UTC().Format(time.RFC3339Nano), ID: last.ID})
		next = &encoded
		items = items[:limit]
	}
	encoded := make([]map[string]any, 0, len(items))
	for _, item := range items {
		encoded = append(encoded, incidentResponse(item))
	}
	writeJSON(response, http.StatusOK, struct {
		Items []map[string]any `json:"items"`
		Next  *string          `json:"next_cursor"`
	}{encoded, next})
}

func (api *authAPI) getIncident(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	item, err := api.alerts.GetIncident(request.Context(), request.PathValue("id"))
	if err != nil {
		api.writeAlertError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, incidentResponse(item))
}

func alertRuleFromInput(input alertRuleInput, current *alerts.Rule) alerts.Rule {
	var rule alerts.Rule
	if current != nil {
		rule = *current
		rule.ChannelIDs = append([]string(nil), current.ChannelIDs...)
	}
	if current != nil && input.Type != nil && *input.Type != current.Type {
		resetAlertCondition(&rule)
	}
	if current != nil && input.Mode != nil && *input.Mode != current.CheckMode {
		rule.ForSeconds = 0
		rule.ConsecutiveFailures = 0
	}
	if input.Name != nil {
		rule.Name = *input.Name
	}
	if input.Type != nil {
		rule.Type = *input.Type
	}
	if input.Severity != nil {
		rule.Severity = *input.Severity
	}
	if input.HostID != nil || input.CheckID != nil || input.FilesystemID != nil {
		rule.HostID = nil
		rule.CheckID = nil
		rule.FilesystemID = nil
		if input.HostID != nil {
			rule.HostID = input.HostID
		}
		if input.CheckID != nil {
			rule.CheckID = input.CheckID
		}
		if input.FilesystemID != nil {
			rule.FilesystemID = input.FilesystemID
		}
	}
	if input.Mode != nil {
		rule.CheckMode = *input.Mode
	}
	if input.ForSeconds != nil {
		rule.ForSeconds = *input.ForSeconds
	}
	if input.ConsecutiveFailures != nil {
		rule.ConsecutiveFailures = *input.ConsecutiveFailures
	}
	if input.Metric != nil {
		rule.Metric = *input.Metric
	}
	if input.Operator != nil {
		rule.Operator = *input.Operator
	}
	if input.Threshold != nil {
		rule.Threshold = *input.Threshold
	}
	if input.ChannelIDs != nil {
		rule.ChannelIDs = append([]string(nil), (*input.ChannelIDs)...)
	}
	return rule
}

func resetAlertCondition(rule *alerts.Rule) {
	rule.HostID = nil
	rule.CheckID = nil
	rule.FilesystemID = nil
	rule.CheckMode = ""
	rule.ForSeconds = 0
	rule.ConsecutiveFailures = 0
	rule.Metric = ""
	rule.Operator = ""
	rule.Threshold = 0
}

func alertRuleResponse(rule alerts.Rule) map[string]any {
	value := map[string]any{
		"id":          rule.ID,
		"name":        rule.Name,
		"type":        rule.Type,
		"severity":    rule.Severity,
		"channel_ids": rule.ChannelIDs,
		"enabled":     rule.Enabled,
		"archived":    rule.ArchivedAt != nil,
		"archived_at": formatTime(rule.ArchivedAt),
		"created_at":  rule.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":  rule.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"version":     rule.Version,
		"evaluation":  alertEvaluationResponse(rule.Evaluation),
	}
	switch rule.Type {
	case alerts.RuleHostUnreachable:
		value["host_id"] = rule.HostID
		value["for_seconds"] = rule.ForSeconds
	case alerts.RuleCheckFailed:
		value["check_id"] = rule.CheckID
		value["mode"] = rule.CheckMode
		if rule.CheckMode == alerts.CheckAttempts {
			value["consecutive_failures"] = rule.ConsecutiveFailures
		} else {
			value["for_seconds"] = rule.ForSeconds
		}
	case alerts.RuleHostMetric:
		value["host_id"] = rule.HostID
		value["metric"] = rule.Metric
		value["operator"] = rule.Operator
		value["threshold"] = rule.Threshold
		value["for_seconds"] = rule.ForSeconds
	case alerts.RuleFilesystem:
		value["filesystem_id"] = rule.FilesystemID
		value["operator"] = rule.Operator
		value["threshold"] = rule.Threshold
		value["for_seconds"] = rule.ForSeconds
	}
	return value
}

func alertEvaluationResponse(evaluation alerts.Evaluation) map[string]any {
	return map[string]any{
		"state":                 evaluation.State,
		"first_true_at":         formatTime(evaluation.FirstTrueAt),
		"last_evaluated_at":     formatTime(evaluation.LastEvaluatedAt),
		"last_value":            evaluation.LastValue,
		"last_condition_status": evaluation.LastConditionStatus,
		"incident_id":           evaluation.IncidentID,
		"updated_at":            formatTime(evaluation.UpdatedAt),
	}
}

func incidentResponse(incident alerts.Incident) map[string]any {
	return map[string]any{
		"id":                incident.ID,
		"rule_id":           incident.RuleID,
		"rule_name":         incident.RuleName,
		"rule_type":         incident.RuleType,
		"target_id":         incident.TargetID,
		"target_name":       incident.TargetName,
		"severity":          incident.Severity,
		"state":             incident.State,
		"condition_status":  incident.ConditionStatus,
		"started_at":        incident.StartedAt.UTC().Format(time.RFC3339Nano),
		"fired_at":          incident.FiredAt.UTC().Format(time.RFC3339Nano),
		"resolved_at":       formatTime(incident.ResolvedAt),
		"initial_value":     incident.InitialValue,
		"last_value":        incident.LastValue,
		"resolution_reason": incident.ResolutionReason,
	}
}

func (api *authAPI) writeAlertError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, alerts.ErrNotFound):
		api.notFound(response, request)
	case errors.Is(err, alerts.ErrVersionConflict):
		writeBareError(response, request, http.StatusPreconditionFailed, "version_conflict", "Resource version does not match")
	case errors.Is(err, alerts.ErrArchived):
		api.writeError(response, request, http.StatusConflict, "rule_archived", "Alert rule is archived", nil)
	case errors.Is(err, alerts.ErrTargetNotFound):
		api.writeError(response, request, http.StatusUnprocessableEntity, "target_not_found", "Alert target was not found", nil)
	case errors.Is(err, alerts.ErrChannelInvalid):
		api.writeError(response, request, http.StatusUnprocessableEntity, "channel_invalid", "Notification channel is invalid", nil)
	case errors.Is(err, alerts.ErrInvalid):
		api.writeError(response, request, http.StatusUnprocessableEntity, "validation_failed", "Alert rule is invalid", nil)
	default:
		api.internal(response, request, err)
	}
}
