package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mkrebs03/atalaya/internal/alerts"
	"github.com/mkrebs03/atalaya/internal/auth"
	"github.com/mkrebs03/atalaya/internal/checks"
	startupconfig "github.com/mkrebs03/atalaya/internal/config"
	"github.com/mkrebs03/atalaya/internal/hostquery"
	"github.com/mkrebs03/atalaya/internal/hosts"
	"github.com/mkrebs03/atalaya/internal/metrics"
	"github.com/mkrebs03/atalaya/internal/notifications"
	settingsdomain "github.com/mkrebs03/atalaya/internal/settings"
	"github.com/mkrebs03/atalaya/internal/system"
)

const sessionCookie = "atalaya_session"

const maxProxyIdentityBytes = 255

const contentSecurityPolicy = "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'"

type AuthConfig struct {
	Listen       string
	Mode         startupconfig.Mode
	PublicURL    string
	TrustedProxy startupconfig.TrustedProxy
	Version      string
	Logger       *slog.Logger
	Clock        func() time.Time
	Random       io.Reader
	RateLimiter  *auth.LoginRateLimiter
}

type authAPI struct {
	service       *auth.Service
	config        AuthConfig
	hosts         hostquery.Repository
	management    hosts.ManagementRepository
	metrics       metrics.Repository
	checks        checks.Repository
	checkMetrics  metrics.CheckRepository
	settings      settingsdomain.Repository
	alerts        alerts.Repository
	notifications notifications.Repository
	system        system.Provider
}

func NewAuthenticated(assets fs.FS, service *auth.Service, config AuthConfig, hostRepositories ...hostquery.Repository) (http.Handler, error) {
	spa, err := New(assets)
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, fmt.Errorf("authentication service is required")
	}
	if err := validateAuthConfig(config); err != nil {
		return nil, fmt.Errorf("validate authentication HTTP configuration: %w", err)
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.RateLimiter == nil {
		config.RateLimiter = auth.NewLoginRateLimiter(config.Clock)
	}
	if len(hostRepositories) > 1 {
		return nil, fmt.Errorf("at most one host repository is allowed")
	}
	api := &authAPI{service: service, config: config}
	if len(hostRepositories) == 1 {
		api.hosts = hostRepositories[0]
		if management, ok := hostRepositories[0].(hosts.ManagementRepository); ok {
			api.management = management
		}
		if repository, ok := hostRepositories[0].(metrics.Repository); ok {
			api.metrics = repository
		}
		if repository, ok := hostRepositories[0].(checks.Repository); ok {
			api.checks = repository
		}
		if repository, ok := hostRepositories[0].(metrics.CheckRepository); ok {
			api.checkMetrics = repository
		}
		if repository, ok := hostRepositories[0].(settingsdomain.Repository); ok {
			api.settings = repository
		}
		if repository, ok := hostRepositories[0].(alerts.Repository); ok {
			api.alerts = repository
		}
		if repository, ok := hostRepositories[0].(notifications.Repository); ok {
			api.notifications = repository
		}
		if repository, ok := hostRepositories[0].(system.Provider); ok {
			api.system = repository
		}
	}
	mux := http.NewServeMux()
	if api.management != nil {
		mux.HandleFunc("POST /api/v1/agent-enrollments", api.enrollAgent)
		mux.HandleFunc("/api/v1/agent-enrollments", api.methodNotAllowed)
	}
	mux.HandleFunc("POST /api/v1/auth/login", api.login)
	mux.HandleFunc("/api/v1/auth/login", api.methodNotAllowed)
	mux.HandleFunc("GET /api/v1/auth/me", api.me)
	mux.HandleFunc("/api/v1/auth/me", api.methodNotAllowed)
	mux.HandleFunc("GET /api/v1/auth/csrf", api.csrf)
	mux.HandleFunc("/api/v1/auth/csrf", api.methodNotAllowed)
	mux.HandleFunc("POST /api/v1/auth/logout", api.logout)
	mux.HandleFunc("/api/v1/auth/logout", api.methodNotAllowed)
	mux.HandleFunc("POST /api/v1/auth/change-password", api.changePassword)
	mux.HandleFunc("/api/v1/auth/change-password", api.methodNotAllowed)
	mux.HandleFunc("GET /api/v1/system/status", api.systemStatus)
	mux.HandleFunc("/api/v1/system/status", api.methodNotAllowed)
	if api.hosts != nil {
		mux.HandleFunc("GET /api/v1/hosts", api.listHosts)
		mux.HandleFunc("HEAD /api/v1/hosts", api.hostsMethodNotAllowed)
		mux.HandleFunc("/api/v1/hosts", api.methodNotAllowed)
		mux.HandleFunc("GET /api/v1/hosts/{id}", api.getHost)
		mux.HandleFunc("GET /api/v1/hosts/{id}/filesystems", api.listFilesystems)
		if api.management != nil {
			mux.HandleFunc("PATCH /api/v1/hosts/{id}", api.patchHost)
			mux.HandleFunc("DELETE /api/v1/hosts/{id}", api.archiveHost)
			mux.HandleFunc("POST /api/v1/hosts/{id}/enable", api.enableHost)
			mux.HandleFunc("POST /api/v1/hosts/{id}/disable", api.disableHost)
			mux.HandleFunc("POST /api/v1/hosts/{id}/restore", api.restoreHost)
			mux.HandleFunc("PATCH /api/v1/hosts/{id}/filesystems/{filesystem_id}", api.selectFilesystem)
		}
		mux.HandleFunc("HEAD /api/v1/hosts/{id}", api.hostsMethodNotAllowed)
		mux.HandleFunc("HEAD /api/v1/hosts/{id}/filesystems", api.hostsMethodNotAllowed)
		mux.HandleFunc("/api/v1/hosts/{id}", api.methodNotAllowed)
		mux.HandleFunc("/api/v1/hosts/{id}/filesystems", api.methodNotAllowed)
		if api.metrics != nil {
			mux.HandleFunc("GET /api/v1/hosts/{id}/metrics", api.getHostMetrics)
			mux.HandleFunc("/api/v1/hosts/{id}/metrics", api.methodNotAllowed)
		}
	}
	if api.management != nil {
		mux.HandleFunc("POST /api/v1/enrollment-tokens", api.createEnrollmentToken)
		mux.HandleFunc("GET /api/v1/enrollment-tokens", api.listEnrollmentTokens)
		mux.HandleFunc("DELETE /api/v1/enrollment-tokens/{id}", api.revokeEnrollmentToken)
		mux.HandleFunc("/api/v1/enrollment-tokens", api.methodNotAllowed)
		mux.HandleFunc("/api/v1/enrollment-tokens/{id}", api.methodNotAllowed)
	}
	if api.metrics != nil || api.settings != nil {
		mux.HandleFunc("GET /api/v1/settings", api.getSettings)
		mux.HandleFunc("PATCH /api/v1/settings", api.patchSettings)
		mux.HandleFunc("/api/v1/settings", api.methodNotAllowed)
	}
	if api.checks != nil {
		mux.HandleFunc("GET /api/v1/checks", api.listChecks)
		mux.HandleFunc("POST /api/v1/checks", api.createCheck)
		mux.HandleFunc("/api/v1/checks", api.methodNotAllowed)
		mux.HandleFunc("GET /api/v1/checks/{id}", api.getCheck)
		mux.HandleFunc("PATCH /api/v1/checks/{id}", api.patchCheck)
		mux.HandleFunc("DELETE /api/v1/checks/{id}", api.archiveCheck)
		mux.HandleFunc("POST /api/v1/checks/{id}/enable", api.enableCheck)
		mux.HandleFunc("POST /api/v1/checks/{id}/disable", api.disableCheck)
		mux.HandleFunc("POST /api/v1/checks/{id}/restore", api.restoreCheck)
		mux.HandleFunc("POST /api/v1/checks/{id}/run", api.runCheck)
		mux.HandleFunc("GET /api/v1/checks/{id}/runs", api.listCheckRuns)
		mux.HandleFunc("GET /api/v1/checks/{id}/runs/{run_id}", api.getCheckRun)
		mux.HandleFunc("/api/v1/checks/{id}", api.methodNotAllowed)
		mux.HandleFunc("/api/v1/checks/{id}/runs", api.methodNotAllowed)
		mux.HandleFunc("/api/v1/checks/{id}/runs/{run_id}", api.methodNotAllowed)
		mux.HandleFunc("/api/v1/checks/{id}/results", api.methodNotAllowed)
		if api.checkMetrics != nil {
			mux.HandleFunc("GET /api/v1/checks/{id}/results", api.getCheckMetrics)
		}
	}
	if api.alerts != nil {
		mux.HandleFunc("GET /api/v1/alert-rules", api.listAlertRules)
		mux.HandleFunc("POST /api/v1/alert-rules", api.createAlertRule)
		mux.HandleFunc("/api/v1/alert-rules", api.methodNotAllowed)
		mux.HandleFunc("GET /api/v1/alert-rules/{id}", api.getAlertRule)
		mux.HandleFunc("PATCH /api/v1/alert-rules/{id}", api.patchAlertRule)
		mux.HandleFunc("DELETE /api/v1/alert-rules/{id}", api.archiveAlertRule)
		mux.HandleFunc("POST /api/v1/alert-rules/{id}/enable", api.enableAlertRule)
		mux.HandleFunc("POST /api/v1/alert-rules/{id}/disable", api.disableAlertRule)
		mux.HandleFunc("POST /api/v1/alert-rules/{id}/restore", api.restoreAlertRule)
		mux.HandleFunc("GET /api/v1/incidents", api.listIncidents)
		mux.HandleFunc("GET /api/v1/incidents/{id}", api.getIncident)
		mux.HandleFunc("/api/v1/incidents", api.methodNotAllowed)
		mux.HandleFunc("/api/v1/incidents/{id}", api.methodNotAllowed)
	}
	if api.notifications != nil {
		mux.HandleFunc("GET /api/v1/notification-channels", api.listNotificationChannels)
		mux.HandleFunc("POST /api/v1/notification-channels", api.createNotificationChannel)
		mux.HandleFunc("/api/v1/notification-channels", api.methodNotAllowed)
		mux.HandleFunc("GET /api/v1/notification-channels/{id}", api.getNotificationChannel)
		mux.HandleFunc("PATCH /api/v1/notification-channels/{id}", api.patchNotificationChannel)
		mux.HandleFunc("DELETE /api/v1/notification-channels/{id}", api.archiveNotificationChannel)
		mux.HandleFunc("POST /api/v1/notification-channels/{id}/restore", api.restoreNotificationChannel)
		mux.HandleFunc("POST /api/v1/notification-channels/{id}/test", api.testNotificationChannel)
		mux.HandleFunc("GET /api/v1/notification-deliveries", api.listNotificationDeliveries)
		mux.HandleFunc("GET /api/v1/notification-deliveries/{id}", api.getNotificationDelivery)
		mux.HandleFunc("/api/v1/notification-channels/{id}", api.methodNotAllowed)
		mux.HandleFunc("/api/v1/notification-deliveries", api.methodNotAllowed)
		mux.HandleFunc("/api/v1/notification-deliveries/{id}", api.methodNotAllowed)
	}
	mux.HandleFunc("/api", api.notFound)
	mux.HandleFunc("/api/", api.notFound)
	mux.Handle("/", api.protectUI(spa))
	return api.middleware(mux), nil
}

func (api *authAPI) hostsMethodNotAllowed(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Allow", http.MethodGet)
	api.methodNotAllowed(response, request)
}

func validateAuthConfig(config AuthConfig) error {
	if err := startupconfig.ValidateServerSettings(config.Listen, config.Mode, config.PublicURL); err != nil {
		return err
	}
	trustedProxy := config.TrustedProxy
	if trustedProxy == "" {
		trustedProxy = startupconfig.TrustedProxyNone
	}
	if trustedProxy != startupconfig.TrustedProxyNone && trustedProxy != startupconfig.TrustedProxyTailscaleServe {
		return fmt.Errorf("unsupported trusted proxy %q", trustedProxy)
	}
	if trustedProxy == startupconfig.TrustedProxyTailscaleServe && config.Mode != startupconfig.ModeProduction {
		return fmt.Errorf("tailscale_serve trusted proxy requires production mode")
	}
	return nil
}

func (api *authAPI) notFound(response http.ResponseWriter, request *http.Request) {
	api.writeError(response, request, http.StatusNotFound, "not_found", "Resource not found", nil)
}

func (api *authAPI) methodNotAllowed(response http.ResponseWriter, request *http.Request) {
	api.writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "Method is not allowed", nil)
}

func (api *authAPI) login(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Origin") != api.config.PublicURL || strings.EqualFold(request.Header.Get("Sec-Fetch-Site"), "cross-site") {
		api.writeError(response, request, http.StatusForbidden, "origin_forbidden", "Request origin is not allowed", nil)
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(response, request, &input); err != nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	normalized, _ := auth.NormalizeUsername(input.Username)
	allowed, retry := api.config.RateLimiter.Allow(normalized)
	if !allowed {
		response.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, int(retry.Seconds()+0.999))))
		api.writeError(response, request, http.StatusTooManyRequests, "rate_limited", "Too many login attempts", nil)
		return
	}
	metadata := api.metadata(request)
	authenticated, err := api.service.Login(request.Context(), input.Username, []byte(input.Password), metadata)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			api.config.RateLimiter.Failure(normalized)
			api.writeError(response, request, http.StatusUnauthorized, "invalid_credentials", "Invalid username or password", nil)
			return
		}
		api.internal(response, request, err)
		return
	}
	api.config.RateLimiter.Success(normalized)
	api.setCookie(response, authenticated.Token, authenticated.Session.AbsoluteExpiresAt)
	api.writeSession(response, request, authenticated)
}

func (api *authAPI) me(response http.ResponseWriter, request *http.Request) {
	authenticated, ok := api.authenticate(response, request)
	if ok {
		api.writeSession(response, request, authenticated)
	}
}
func (api *authAPI) csrf(response http.ResponseWriter, request *http.Request) {
	authenticated, ok := api.authenticate(response, request)
	if !ok {
		return
	}
	token, err := auth.CSRFToken(authenticated.Token)
	if err != nil {
		api.internal(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"csrf_token": token})
}
func (api *authAPI) logout(response http.ResponseWriter, request *http.Request) {
	authenticated, ok := api.authenticate(response, request)
	if !ok {
		return
	}
	if !auth.VerifyCSRF(authenticated.Token, request.Header.Get("X-CSRF-Token")) {
		api.writeError(response, request, http.StatusForbidden, "csrf_failed", "CSRF validation failed", nil)
		return
	}
	if err := api.service.Logout(request.Context(), authenticated.Token); err != nil {
		if errors.Is(err, auth.ErrAuthenticationRequired) {
			api.clearCookie(response)
			api.writeError(response, request, http.StatusUnauthorized, "authentication_required", "Authentication is required", nil)
			return
		}
		api.internal(response, request, err)
		return
	}
	api.clearCookie(response)
	response.WriteHeader(http.StatusNoContent)
}
func (api *authAPI) changePassword(response http.ResponseWriter, request *http.Request) {
	authenticated, ok := api.authenticate(response, request)
	if !ok {
		return
	}
	if !auth.VerifyCSRF(authenticated.Token, request.Header.Get("X-CSRF-Token")) {
		api.writeError(response, request, http.StatusForbidden, "csrf_failed", "CSRF validation failed", nil)
		return
	}
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if decodeJSON(response, request, &input) != nil {
		api.writeError(response, request, http.StatusBadRequest, "validation_failed", "Request is invalid", nil)
		return
	}
	changed, err := api.service.ChangePassword(request.Context(), authenticated.Token, []byte(input.CurrentPassword), []byte(input.NewPassword), api.metadata(request))
	if errors.Is(err, auth.ErrCurrentPasswordInvalid) {
		api.writeError(response, request, http.StatusForbidden, "current_password_invalid", "Current password is invalid", nil)
		return
	}
	if errors.Is(err, auth.ErrNewPasswordInvalid) {
		api.writeError(response, request, http.StatusUnprocessableEntity, "validation_failed", "New password is invalid", nil)
		return
	}
	if errors.Is(err, auth.ErrAuthenticationRequired) || errors.Is(err, auth.ErrSessionExpired) {
		api.clearCookie(response)
		code := "authentication_required"
		if errors.Is(err, auth.ErrSessionExpired) {
			code = "session_expired"
		}
		api.writeError(response, request, http.StatusUnauthorized, code, "Authentication is required", nil)
		return
	}
	if err != nil {
		api.internal(response, request, err)
		return
	}
	api.setCookie(response, changed.Token, changed.Session.AbsoluteExpiresAt)
	api.writeSession(response, request, changed)
}

func (api *authAPI) authenticate(response http.ResponseWriter, request *http.Request) (auth.AuthenticatedSession, bool) {
	cookie, err := request.Cookie(sessionCookie)
	if err != nil {
		api.writeError(response, request, http.StatusUnauthorized, "authentication_required", "Authentication is required", nil)
		return auth.AuthenticatedSession{}, false
	}
	authenticated, err := api.service.Authenticate(request.Context(), cookie.Value)
	if err != nil {
		if !errors.Is(err, auth.ErrSessionExpired) && !errors.Is(err, auth.ErrAuthenticationRequired) {
			api.internal(response, request, err)
			return auth.AuthenticatedSession{}, false
		}
		api.clearCookie(response)
		code := "authentication_required"
		if errors.Is(err, auth.ErrSessionExpired) {
			code = "session_expired"
		}
		api.writeError(response, request, http.StatusUnauthorized, code, "Authentication is required", nil)
		return auth.AuthenticatedSession{}, false
	}
	return authenticated, true
}

func (api *authAPI) protectUI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/healthz" || request.URL.Path == "/login" || isAssetPath(request.URL.Path) {
			next.ServeHTTP(response, request)
			return
		}
		if isAPIPath(request.URL.Path) || hasTraversalSegment(request.URL.Path) || looksLikeFilePath(request.URL.Path) {
			next.ServeHTTP(response, request)
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			next.ServeHTTP(response, request)
			return
		}
		cookie, err := request.Cookie(sessionCookie)
		if err != nil {
			http.Redirect(response, request, "/login", http.StatusSeeOther)
			return
		}
		if _, err := api.service.Authenticate(request.Context(), cookie.Value); err != nil {
			api.clearCookie(response)
			http.Redirect(response, request, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (api *authAPI) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		api.setSecurityHeaders(response, request)
		idBytes := make([]byte, 16)
		if _, err := io.ReadFull(api.config.Random, idBytes); err != nil {
			const unavailableRequestID = "unavailable"
			request = request.WithContext(withRequestID(request.Context(), unavailableRequestID))
			response.Header().Set("X-Request-ID", unavailableRequestID)
			api.internal(response, request, fmt.Errorf("generate request ID: %w", err))
			return
		}
		requestID := hex.EncodeToString(idBytes)
		request = request.WithContext(withRequestID(request.Context(), requestID))
		response.Header().Set("X-Request-ID", requestID)
		defer func() {
			if recovered := recover(); recovered != nil {
				api.config.Logger.Error("HTTP handler panic", "request_id", requestID)
				api.writeError(response, request, 500, "internal_error", "Internal server error", nil)
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func (api *authAPI) setSecurityHeaders(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Security-Policy", contentSecurityPolicy)
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	if isAPIPath(request.URL.Path) {
		response.Header().Set("Cache-Control", "no-store")
	}
}

func (api *authAPI) metadata(request *http.Request) auth.SessionMetadata {
	metadata := auth.SessionMetadata{RemoteAddr: request.RemoteAddr, UserAgent: request.UserAgent()}
	if api.config.Mode != startupconfig.ModeProduction || api.config.TrustedProxy != startupconfig.TrustedProxyTailscaleServe || !remoteLoopback(request.RemoteAddr) {
		return metadata
	}
	decoder := new(mime.WordDecoder)
	login, loginErr := decoder.DecodeHeader(request.Header.Get("Tailscale-User-Login"))
	name, nameErr := decoder.DecodeHeader(request.Header.Get("Tailscale-User-Name"))
	if loginErr == nil && login != "" {
		if validProxyIdentity(login) {
			metadata.TailscaleLogin = &login
		}
	}
	if nameErr == nil && name != "" {
		if validProxyIdentity(name) {
			metadata.TailscaleName = &name
		}
	}
	metadata.ProxyIdentityInformational = metadata.TailscaleLogin != nil || metadata.TailscaleName != nil
	return metadata
}
func validProxyIdentity(value string) bool {
	if len(value) > maxProxyIdentityBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
func remoteLoopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) error {
	content := request.Header.Get("Content-Type")
	media, _, err := mime.ParseMediaType(content)
	if err != nil || media != "application/json" {
		return fmt.Errorf("invalid content type")
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4096)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func (api *authAPI) writeSession(response http.ResponseWriter, request *http.Request, value auth.AuthenticatedSession) {
	writeJSON(response, http.StatusOK, map[string]any{"administrator": map[string]string{"id": value.Administrator.ID, "username": value.Administrator.Username}, "session": map[string]string{"idle_expires_at": value.Session.IdleExpiresAt.UTC().Format(time.RFC3339Nano), "absolute_expires_at": value.Session.AbsoluteExpiresAt.UTC().Format(time.RFC3339Nano)}})
}
func (api *authAPI) setCookie(response http.ResponseWriter, token string, expiry time.Time) {
	secure := api.config.Mode == startupconfig.ModeProduction
	http.SetCookie(response, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, Expires: expiry, MaxAge: max(1, int(expiry.Sub(api.config.Clock()).Seconds()))})
}
func (api *authAPI) clearCookie(response http.ResponseWriter) {
	http.SetCookie(response, &http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true, Secure: api.config.Mode == startupconfig.ModeProduction, SameSite: http.SameSiteStrictMode, Expires: time.Unix(1, 0), MaxAge: -1})
}
func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
func (api *authAPI) writeError(response http.ResponseWriter, request *http.Request, status int, code, message string, fields map[string]string) {
	errorBody := map[string]any{"code": code, "message": message, "request_id": requestID(request.Context())}
	if fields != nil {
		errorBody["fields"] = fields
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if request.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(response).Encode(map[string]any{"error": errorBody})
}
func (api *authAPI) internal(response http.ResponseWriter, request *http.Request, err error) {
	api.config.Logger.Error("HTTP request failed", "request_id", requestID(request.Context()), "error", err.Error())
	api.writeError(response, request, 500, "internal_error", "Internal server error", nil)
}
