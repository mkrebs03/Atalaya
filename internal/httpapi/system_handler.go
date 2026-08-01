package httpapi

import (
	"net/http"
)

func (api *authAPI) systemStatus(response http.ResponseWriter, request *http.Request) {
	if _, ok := api.authenticate(response, request); !ok {
		return
	}
	if api.system == nil {
		api.writeError(response, request, http.StatusServiceUnavailable, "system_status_unavailable", "System status is unavailable", nil)
		return
	}
	status, err := api.system.SystemStatus(request.Context(), api.config.Version)
	if err != nil {
		api.internal(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, status)
}
