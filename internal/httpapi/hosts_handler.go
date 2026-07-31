package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mkrebs03/atalaya/internal/hostquery"
)

type pageCursor struct {
	First string `json:"first"`
	ID    string `json:"id"`
}

func (api *authAPI) listHosts(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	limit, cursor, ok := api.pageParameters(response, request, "include_archived")
	if !ok {
		return
	}
	includeArchived := request.URL.Query().Get("include_archived") == "true"
	var after *hostquery.HostCursor
	if cursor != nil {
		after = &hostquery.HostCursor{Name: cursor.First, ID: cursor.ID}
	}
	var items []hostquery.HostSummary
	var err error
	if includeArchived && api.management != nil {
		items, err = api.management.ListHostsIncludingArchived(request.Context(), limit+1, after)
	} else {
		items, err = api.hosts.ListHosts(request.Context(), limit+1, after)
	}
	if err != nil {
		api.internal(response, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		last := items[limit-1]
		encoded := encodeCursor(pageCursor{First: normalizeNoCase(last.DisplayName), ID: last.ID})
		next = &encoded
		items = items[:limit]
	}
	writeJSON(response, http.StatusOK, struct {
		Items []hostquery.HostSummary `json:"items"`
		Next  *string                 `json:"next_cursor"`
	}{items, next})
}

func (api *authAPI) getHost(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	id := request.PathValue("id")
	if id == "" {
		api.notFound(response, request)
		return
	}
	item, err := api.hosts.GetHost(request.Context(), id)
	if errors.Is(err, hostquery.ErrNotFound) {
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
	writeJSON(response, http.StatusOK, item)
}

func (api *authAPI) listFilesystems(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	limit, cursor, ok := api.pageParameters(response, request)
	if !ok {
		return
	}
	var after *hostquery.FilesystemCursor
	if cursor != nil {
		after = &hostquery.FilesystemCursor{MountPoint: cursor.First, ID: cursor.ID}
	}
	items, err := api.hosts.ListFilesystems(request.Context(), request.PathValue("id"), limit+1, after)
	if errors.Is(err, hostquery.ErrNotFound) {
		api.notFound(response, request)
		return
	}
	if err != nil {
		api.internal(response, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		last := items[limit-1]
		encoded := encodeCursor(pageCursor{First: last.MountPoint, ID: last.ID})
		next = &encoded
		items = items[:limit]
	}
	writeJSON(response, http.StatusOK, struct {
		Items []hostquery.Filesystem `json:"items"`
		Next  *string                `json:"next_cursor"`
	}{items, next})
}

func (api *authAPI) pageParameters(response http.ResponseWriter, request *http.Request, extras ...string) (int, *pageCursor, bool) {
	limit := 50
	fields := map[string]string{}
	allowed := map[string]bool{}
	for _, extra := range extras {
		allowed[extra] = true
	}
	if values, exists := request.URL.Query()["limit"]; exists {
		if len(values) != 1 {
			fields["limit"] = "must be a single integer between 1 and 100"
		} else if parsed, err := strconv.Atoi(values[0]); err != nil || parsed < 1 || parsed > 100 {
			fields["limit"] = "must be an integer between 1 and 100"
		} else {
			limit = parsed
		}
	}
	var cursor *pageCursor
	if values, exists := request.URL.Query()["cursor"]; exists {
		if len(values) != 1 || values[0] == "" {
			fields["cursor"] = "must be a valid opaque cursor"
		} else if decoded, ok := decodeCursor(values[0]); !ok {
			fields["cursor"] = "must be a valid opaque cursor"
		} else {
			cursor = &decoded
		}
	}
	for key := range request.URL.Query() {
		if key != "limit" && key != "cursor" && !allowed[key] {
			fields[key] = "is not supported"
		}
	}
	if allowed["include_archived"] {
		if values, exists := request.URL.Query()["include_archived"]; exists && (len(values) != 1 || (values[0] != "true" && values[0] != "false")) {
			fields["include_archived"] = "must be true or false"
		}
	}
	if len(fields) > 0 {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", fields)
		return 0, nil, false
	}
	return limit, cursor, true
}

func encodeCursor(cursor pageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodeCursor(value string) (pageCursor, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 2048 {
		return pageCursor{}, false
	}
	var cursor pageCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil || cursor.First == "" || cursor.ID == "" {
		return pageCursor{}, false
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		return pageCursor{}, false
	}
	return cursor, true
}

// SQLite NOCASE folds ASCII only; cursor normalization must match its ordering.
func normalizeNoCase(value string) string {
	var normalized strings.Builder
	normalized.Grow(len(value))
	for _, character := range value {
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		normalized.WriteRune(character)
	}
	return normalized.String()
}

func ifNoneMatch(headers []string, current string) bool {
	for _, header := range headers {
		for _, candidate := range strings.Split(header, ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate == "*" {
				return true
			}
			// If-None-Match uses weak comparison for GET/HEAD.
			if strings.HasPrefix(candidate, "W/") {
				candidate = strings.TrimSpace(strings.TrimPrefix(candidate, "W/"))
			}
			if candidate == current {
				return true
			}
		}
	}
	return false
}
