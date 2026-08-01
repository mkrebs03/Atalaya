package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mkrebs03/atalaya/internal/notifications"
)

type notificationChannelInput struct {
	Name       *string                 `json:"name"`
	Provider   *notifications.Provider `json:"provider"`
	WebhookURL *string                 `json:"webhook_url"`
	Enabled    *bool                   `json:"enabled"`
}

type notificationTestInput struct {
	RequestID string `json:"request_id"`
}

func (api *authAPI) listNotificationChannels(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	limit, cursor, ok := api.pageParameters(response, request, "include_archived")
	if !ok {
		return
	}
	includeArchived := request.URL.Query().Get("include_archived") == "true"
	var after *notifications.ChannelCursor
	if cursor != nil {
		after = &notifications.ChannelCursor{Name: cursor.First, ID: cursor.ID}
	}
	items, err := api.notifications.ListChannels(request.Context(), limit+1, after, includeArchived)
	if err != nil {
		api.writeNotificationError(response, request, err)
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
		encoded = append(encoded, notificationChannelResponse(item))
	}
	writeJSON(response, http.StatusOK, struct {
		Items []map[string]any `json:"items"`
		Next  *string          `json:"next_cursor"`
	}{encoded, next})
}

func (api *authAPI) getNotificationChannel(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	item, err := api.notifications.GetChannel(request.Context(), request.PathValue("id"))
	if err != nil {
		api.writeNotificationError(response, request, err)
		return
	}
	etag := fmt.Sprintf("\"%d\"", item.Version)
	response.Header().Set("ETag", etag)
	if ifNoneMatch(request.Header.Values("If-None-Match"), etag) {
		response.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(response, http.StatusOK, notificationChannelResponse(item))
}

func (api *authAPI) createNotificationChannel(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	var input notificationChannelInput
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	channel := notifications.Channel{Provider: valueOrProvider(input.Provider), Enabled: true}
	if input.Name != nil {
		channel.Name = *input.Name
	}
	if input.Enabled != nil {
		channel.Enabled = *input.Enabled
	}
	if input.WebhookURL == nil {
		api.writeError(response, request, http.StatusUnprocessableEntity, "validation_failed", "Webhook URL is required", nil)
		return
	}
	created, err := api.notifications.CreateChannel(request.Context(), channel, *input.WebhookURL, api.config.Clock().UTC(), notifications.NewReaderIDSource(api.config.Random))
	if err != nil {
		api.writeNotificationError(response, request, err)
		return
	}
	response.Header().Set("ETag", fmt.Sprintf("\"%d\"", created.Version))
	writeJSON(response, http.StatusCreated, notificationChannelResponse(created))
}

func (api *authAPI) patchNotificationChannel(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	version, ok := requireIfMatch(response, request)
	if !ok {
		return
	}
	var input notificationChannelInput
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	current, err := api.notifications.GetChannel(request.Context(), request.PathValue("id"))
	if err != nil {
		api.writeNotificationError(response, request, err)
		return
	}
	next := current
	if input.Name != nil {
		next.Name = *input.Name
	}
	if input.Provider != nil {
		next.Provider = *input.Provider
	}
	if input.Enabled != nil {
		next.Enabled = *input.Enabled
	}
	if err := api.notifications.UpdateChannel(request.Context(), current.ID, version, next, input.WebhookURL, api.config.Clock().UTC()); err != nil {
		api.writeNotificationError(response, request, err)
		return
	}
	api.writeUpdatedNotificationChannel(response, request, current.ID)
}

func (api *authAPI) archiveNotificationChannel(response http.ResponseWriter, request *http.Request) {
	api.notificationChannelAction(response, request, func(id string, version int64) error {
		return api.notifications.ArchiveChannel(request.Context(), id, version, api.config.Clock().UTC())
	}, true)
}

func (api *authAPI) restoreNotificationChannel(response http.ResponseWriter, request *http.Request) {
	api.notificationChannelAction(response, request, func(id string, version int64) error {
		return api.notifications.RestoreChannel(request.Context(), id, version, api.config.Clock().UTC())
	}, false)
}

func (api *authAPI) notificationChannelAction(response http.ResponseWriter, request *http.Request, action func(string, int64) error, noContent bool) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	version, ok := requireIfMatch(response, request)
	if !ok {
		return
	}
	if err := action(request.PathValue("id"), version); err != nil {
		api.writeNotificationError(response, request, err)
		return
	}
	if noContent {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	api.writeUpdatedNotificationChannel(response, request, request.PathValue("id"))
}

func (api *authAPI) writeUpdatedNotificationChannel(response http.ResponseWriter, request *http.Request, id string) {
	item, err := api.notifications.GetChannel(request.Context(), id)
	if err != nil {
		api.writeNotificationError(response, request, err)
		return
	}
	response.Header().Set("ETag", fmt.Sprintf("\"%d\"", item.Version))
	writeJSON(response, http.StatusOK, notificationChannelResponse(item))
}

func (api *authAPI) testNotificationChannel(response http.ResponseWriter, request *http.Request) {
	session, ok := api.authenticate(response, request)
	if !ok || !api.verifyCSRF(response, request, session) {
		return
	}
	var input notificationTestInput
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	delivery, _, err := api.notifications.EnqueueTest(request.Context(), request.PathValue("id"), input.RequestID, api.config.Clock().UTC(), notifications.NewReaderIDSource(api.config.Random))
	if err != nil {
		api.writeNotificationError(response, request, err)
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]any{"delivery_id": delivery.ID, "status": "queued"})
}

func (api *authAPI) listNotificationDeliveries(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	limit, cursor, ok := api.pageParameters(response, request, "channel_id", "state")
	if !ok {
		return
	}
	channelID := request.URL.Query().Get("channel_id")
	state := request.URL.Query().Get("state")
	if state != "" {
		valid := false
		for _, candidate := range []notifications.DeliveryState{notifications.DeliveryPending, notifications.DeliverySending, notifications.DeliveryDelivered, notifications.DeliveryFailed, notifications.DeliverySkipped} {
			if state == string(candidate) {
				valid = true
				break
			}
		}
		if !valid {
			api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", map[string]string{"state": "must be a valid delivery state"})
			return
		}
	}
	var after *notifications.DeliveryCursor
	if cursor != nil {
		if _, err := time.Parse(time.RFC3339Nano, cursor.First); err != nil {
			api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", map[string]string{"cursor": "must be a valid opaque cursor"})
			return
		}
		after = &notifications.DeliveryCursor{CreatedAt: cursor.First, ID: cursor.ID}
	}
	items, err := api.notifications.ListDeliveries(request.Context(), channelID, state, limit+1, after)
	if err != nil {
		api.writeNotificationError(response, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		last := items[limit-1]
		encoded := encodeCursor(pageCursor{First: last.CreatedAt.UTC().Format(time.RFC3339Nano), ID: last.ID})
		next = &encoded
		items = items[:limit]
	}
	encoded := make([]map[string]any, 0, len(items))
	for _, item := range items {
		encoded = append(encoded, notificationDeliveryResponse(item))
	}
	writeJSON(response, http.StatusOK, struct {
		Items []map[string]any `json:"items"`
		Next  *string          `json:"next_cursor"`
	}{encoded, next})
}

func (api *authAPI) getNotificationDelivery(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	item, err := api.notifications.GetDelivery(request.Context(), request.PathValue("id"))
	if err != nil {
		api.writeNotificationError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, notificationDeliveryResponse(item))
}

func valueOrProvider(value *notifications.Provider) notifications.Provider {
	if value == nil {
		return ""
	}
	return *value
}

func notificationChannelResponse(channel notifications.Channel) map[string]any {
	return map[string]any{
		"id":                channel.ID,
		"name":              channel.Name,
		"provider":          channel.Provider,
		"secret_configured": channel.SecretConfigured,
		"enabled":           channel.Enabled,
		"archived":          channel.ArchivedAt != nil,
		"archived_at":       formatTime(channel.ArchivedAt),
		"last_test_at":      formatTime(channel.LastTestAt),
		"created_at":        channel.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":        channel.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"version":           channel.Version,
	}
}

func notificationDeliveryResponse(delivery notifications.Delivery) map[string]any {
	return map[string]any{
		"id":              delivery.ID,
		"kind":            delivery.Kind,
		"channel_id":      delivery.ChannelID,
		"incident_id":     delivery.IncidentID,
		"transition":      delivery.Transition,
		"test_request_id": delivery.TestRequestID,
		"state":           delivery.State,
		"attempts":        delivery.Attempts,
		"next_attempt_at": delivery.NextAttemptAt.UTC().Format(time.RFC3339Nano),
		"lease_until":     formatTime(delivery.LeaseUntil),
		"last_error_code": delivery.LastErrorCode,
		"skipped_reason":  delivery.SkippedReason,
		"created_at":      delivery.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":      delivery.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"delivered_at":    formatTime(delivery.DeliveredAt),
	}
}

func (api *authAPI) writeNotificationError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, notifications.ErrNotFound):
		api.notFound(response, request)
	case errors.Is(err, notifications.ErrVersionConflict):
		writeBareError(response, request, http.StatusPreconditionFailed, "version_conflict", "Resource version does not match")
	case errors.Is(err, notifications.ErrArchived):
		api.writeError(response, request, http.StatusConflict, "channel_archived", "Notification channel is archived", nil)
	case errors.Is(err, notifications.ErrInvalid):
		api.writeError(response, request, http.StatusUnprocessableEntity, "validation_failed", "Notification channel is invalid", nil)
	default:
		api.internal(response, request, err)
	}
}
