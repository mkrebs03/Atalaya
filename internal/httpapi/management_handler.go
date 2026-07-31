package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mkrebs03/atalaya/internal/auth"
	"github.com/mkrebs03/atalaya/internal/hostquery"
	"github.com/mkrebs03/atalaya/internal/hosts"
)

func (api *authAPI) enrollAgent(response http.ResponseWriter, request *http.Request) {
	allowed, retry := api.config.RateLimiter.Allow("agent-enrollment:" + request.RemoteAddr)
	if !allowed {
		response.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Seconds()+0.999))))
		api.writeError(response, request, http.StatusTooManyRequests, "rate_limited", "Too many enrollment attempts", nil)
		return
	}
	var input struct {
		EnrollmentToken string `json:"enrollment_token"`
		RequestID       string `json:"request_id"`
		DisplayName     string `json:"display_name"`
		AgentURL        string `json:"agent_url"`
		AgentVersion    string `json:"agent_version"`
		ProtocolVersion int    `json:"protocol_version"`
	}
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	result, err := api.management.EnrollAgent(request.Context(), hosts.EnrollmentRequest{
		Token: input.EnrollmentToken, RequestID: input.RequestID, DisplayName: input.DisplayName,
		AgentURL: input.AgentURL, AgentVersion: input.AgentVersion, ProtocolVersion: input.ProtocolVersion,
	}, api.config.Clock(), api.config.Random)
	if err != nil {
		api.writeEnrollmentError(response, request, err)
		return
	}
	defer clear(result.AgentCredential)
	writeJSON(response, http.StatusOK, map[string]any{
		"host_id":               result.HostID,
		"agent_credential":      base64.RawURLEncoding.EncodeToString(result.AgentCredential),
		"poll_interval_seconds": result.PollIntervalSeconds,
	})
}

func (api *authAPI) createEnrollmentToken(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	var input struct {
		Label string `json:"label"`
	}
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	token, secret, err := api.management.CreateEnrollmentToken(request.Context(), input.Label, session.Administrator.ID, api.config.Clock(), api.config.Random)
	if err != nil {
		api.writeManagementError(response, request, err)
		return
	}
	defer clear([]byte(secret))
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusCreated, map[string]any{
		"id": token.ID, "label": token.Label, "created_at": token.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		"expires_at": token.ExpiresAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), "token": secret, "version": token.Version,
	})
}

func (api *authAPI) listEnrollmentTokens(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	limit, cursor, ok := api.pageParameters(response, request)
	if !ok {
		return
	}
	var after string
	if cursor != nil {
		after = cursor.First
	}
	items, err := api.management.ListEnrollmentTokens(request.Context(), limit+1, after)
	if err != nil {
		api.internal(response, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		last := items[limit-1]
		encoded := encodeCursor(pageCursor{First: last.ID, ID: last.ID})
		next = &encoded
		items = items[:limit]
	}
	view := make([]map[string]any, 0, len(items))
	for _, item := range items {
		value := map[string]any{
			"id": item.ID, "label": item.Label, "created_at": item.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
			"expires_at": item.ExpiresAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), "revoked_at": formatTime(item.RevokedAt),
			"consumed_at": formatTime(item.ConsumedAt), "replay_until": formatTime(item.ReplayUntil), "host_id": item.HostID, "version": item.Version,
		}
		view = append(view, value)
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": view, "next_cursor": next})
}

func (api *authAPI) revokeEnrollmentToken(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	version, ok := requireIfMatch(response, request)
	if !ok {
		return
	}
	if err := api.management.RevokeEnrollmentToken(request.Context(), request.PathValue("id"), version, api.config.Clock()); err != nil {
		api.writeManagementError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *authAPI) patchHost(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	version, ok := requireIfMatch(response, request)
	if !ok {
		return
	}
	var input hosts.HostPatch
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, 400, "validation_failed", "Request is invalid", nil)
		return
	}
	if err := api.management.PatchHost(request.Context(), request.PathValue("id"), version, input, api.config.Clock(), api.config.Random); err != nil {
		api.writeManagementError(response, request, err)
		return
	}
	api.writeUpdatedHost(response, request, request.PathValue("id"))
}

func (api *authAPI) archiveHost(response http.ResponseWriter, request *http.Request) {
	api.hostAction(response, request, "archive")
}

func (api *authAPI) enableHost(response http.ResponseWriter, request *http.Request) {
	api.hostAction(response, request, "enable")
}

func (api *authAPI) disableHost(response http.ResponseWriter, request *http.Request) {
	api.hostAction(response, request, "disable")
}

func (api *authAPI) hostAction(response http.ResponseWriter, request *http.Request, action string) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	version, ok := requireIfMatch(response, request)
	if !ok {
		return
	}
	if err := api.management.SetHostAdministrativeState(request.Context(), request.PathValue("id"), version, action, api.config.Clock(), api.config.Random); err != nil {
		api.writeManagementError(response, request, err)
		return
	}
	if action == "archive" {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	api.writeUpdatedHost(response, request, request.PathValue("id"))
}

func (api *authAPI) restoreHost(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	version, ok := requireIfMatch(response, request)
	if !ok {
		return
	}
	if err := api.management.RestoreHost(request.Context(), request.PathValue("id"), version, api.config.Clock()); err != nil {
		api.writeManagementError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *authAPI) selectFilesystem(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	version, ok := requireIfMatch(response, request)
	if !ok {
		return
	}
	var input struct {
		Selected bool `json:"selected"`
	}
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, 400, "validation_failed", "Request is invalid", nil)
		return
	}
	if err := api.management.SelectFilesystem(request.Context(), request.PathValue("id"), request.PathValue("filesystem_id"), version, input.Selected, api.config.Clock()); err != nil {
		api.writeManagementError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *authAPI) writeUpdatedHost(response http.ResponseWriter, request *http.Request, id string) {
	item, err := api.hosts.GetHost(request.Context(), id)
	if errors.Is(err, hostquery.ErrNotFound) {
		api.notFound(response, request)
		return
	}
	if err != nil {
		api.internal(response, request, err)
		return
	}
	response.Header().Set("ETag", fmt.Sprintf("\"%d\"", item.Version))
	writeJSON(response, http.StatusOK, item)
}

func (api *authAPI) verifyCSRF(response http.ResponseWriter, request *http.Request, session auth.AuthenticatedSession) bool {
	if !auth.VerifyCSRF(session.Token, request.Header.Get("X-CSRF-Token")) {
		api.writeError(response, request, http.StatusForbidden, "csrf_failed", "CSRF validation failed", nil)
		return false
	}
	return true
}

func requireIfMatch(response http.ResponseWriter, request *http.Request) (int64, bool) {
	value := strings.TrimSpace(request.Header.Get("If-Match"))
	if value == "" {
		writeBareError(response, request, http.StatusPreconditionRequired, "version_required", "If-Match is required")
		return 0, false
	}
	if len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' {
		writeBareError(response, request, http.StatusPreconditionFailed, "version_conflict", "Resource version does not match")
		return 0, false
	}
	version, err := strconv.ParseInt(value[1:len(value)-1], 10, 64)
	if err != nil || version < 1 {
		writeBareError(response, request, http.StatusPreconditionFailed, "version_conflict", "Resource version does not match")
		return 0, false
	}
	return version, true
}

func (api *authAPI) writeEnrollmentError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, hosts.ErrEnrollmentInvalid):
		api.writeError(response, request, http.StatusUnauthorized, "authentication_required", "Enrollment token is invalid", nil)
	case errors.Is(err, hosts.ErrEnrollmentConsumed):
		api.writeError(response, request, http.StatusConflict, "enrollment_token_consumed", "Enrollment token has already been consumed", nil)
	case errors.Is(err, hosts.ErrEnrollmentConflict), errors.Is(err, hosts.ErrHostConflict):
		api.writeError(response, request, http.StatusConflict, "conflict", "Enrollment conflicts with an existing host", nil)
	case errors.Is(err, hosts.ErrInvalidManagement):
		api.writeError(response, request, http.StatusUnprocessableEntity, "validation_failed", "Enrollment request is invalid", nil)
	default:
		api.internal(response, request, err)
	}
}

func (api *authAPI) writeManagementError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, hostquery.ErrNotFound), errors.Is(err, hosts.ErrFilesystemNotFound):
		api.notFound(response, request)
	case errors.Is(err, hosts.ErrHostVersionConflict):
		writeBareError(response, request, http.StatusPreconditionFailed, "version_conflict", "Resource version does not match")
	case errors.Is(err, hosts.ErrHostArchived):
		api.writeError(response, request, http.StatusConflict, "host_archived", "Host is archived", nil)
	case errors.Is(err, hosts.ErrHostConflict):
		api.writeError(response, request, http.StatusConflict, "conflict", "Resource conflicts with an existing host", nil)
	case errors.Is(err, hosts.ErrInvalidManagement):
		api.writeError(response, request, http.StatusUnprocessableEntity, "validation_failed", "Request is invalid", nil)
	case errors.Is(err, hosts.ErrEnrollmentInvalid):
		api.writeError(response, request, http.StatusNotFound, "not_found", "Resource not found", nil)
	default:
		api.internal(response, request, err)
	}
}

func writeBareError(response http.ResponseWriter, request *http.Request, status int, code, message string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if request.Method != http.MethodHead {
		_ = json.NewEncoder(response).Encode(map[string]any{"error": map[string]any{"code": code, "message": message, "request_id": requestID(request.Context())}})
	}
}

func formatTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
