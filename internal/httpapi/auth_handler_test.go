package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mkrebs03/atalaya/internal/auth"
	startupconfig "github.com/mkrebs03/atalaya/internal/config"
)

type httpAuthRepository struct {
	administrator auth.Administrator
	session       auth.Session
	validateCalls int
	validateAfter int
	validateError error
	changeError   error
	revokeError   error
}

func (repository *httpAuthRepository) FindActiveAdministrator(_ context.Context, username string) (auth.Administrator, error) {
	if username != repository.administrator.Username {
		return auth.Administrator{}, errors.New("missing")
	}
	return repository.administrator, nil
}
func (repository *httpAuthRepository) CreateSessionIfAdministratorVersion(_ context.Context, session auth.Session, version int) error {
	if version != repository.administrator.Version {
		return errors.New("version")
	}
	repository.session = session
	return nil
}
func (repository *httpAuthRepository) ValidateAndTouchSession(_ context.Context, hash []byte, now, idle time.Time) (auth.Administrator, auth.Session, error) {
	repository.validateCalls++
	if repository.validateError != nil && repository.validateCalls > repository.validateAfter {
		return auth.Administrator{}, auth.Session{}, repository.validateError
	}
	if !bytes.Equal(hash, repository.session.TokenHash) {
		return auth.Administrator{}, auth.Session{}, auth.ErrAuthenticationRequired
	}
	if !now.Before(repository.session.IdleExpiresAt) || !now.Before(repository.session.AbsoluteExpiresAt) {
		return auth.Administrator{}, auth.Session{}, auth.ErrSessionExpired
	}
	repository.session.LastActivityAt = now
	if idle.After(repository.session.AbsoluteExpiresAt) {
		idle = repository.session.AbsoluteExpiresAt
	}
	repository.session.IdleExpiresAt = idle
	return repository.administrator, repository.session, nil
}
func (repository *httpAuthRepository) RevokeSession(context.Context, []byte, time.Time) error {
	if repository.revokeError != nil {
		return repository.revokeError
	}
	repository.session.RevokedAt = &time.Time{}
	repository.session.TokenHash = nil
	return nil
}

func TestLogoutTreatsConcurrentRevocationAsAuthenticationRequired(t *testing.T) {
	hash, err := auth.HashPasswordWithRandom([]byte("correct horse battery staple"), bytes.NewReader(bytes.Repeat([]byte{1}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	repository := &httpAuthRepository{administrator: auth.Administrator{ID: "00000000-0000-4000-8000-000000000001", Username: "admin", PasswordHash: hash, Version: 1}, revokeError: auth.ErrAuthenticationRequired}
	service, err := auth.NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(fstest.MapFS{"index.html": {Data: []byte("app")}}, service, AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeProduction, PublicURL: "https://atalaya.example", Random: bytes.NewReader(bytes.Repeat([]byte{2}, 256))})
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest(http.MethodPost, "https://atalaya.example/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"correct horse battery staple"}`))
	login.Header.Set("Origin", "https://atalaya.example")
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	cookie := loginResponse.Result().Cookies()[0]
	csrf, err := auth.CSRFToken(cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://atalaya.example/api/v1/auth/logout", nil)
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"authentication_required"`) {
		t.Fatalf("logout = %d %s", response.Code, response.Body.String())
	}
	cleared := response.Result().Cookies()
	if len(cleared) != 1 || cleared[0].Name != sessionCookie || cleared[0].MaxAge >= 0 {
		t.Fatalf("logout cookie = %+v", cleared)
	}
}

func TestRequestIDEntropyFailureReturnsUniformAPIError(t *testing.T) {
	service, err := auth.NewService(&httpAuthRepository{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(fstest.MapFS{"index.html": {Data: []byte("app")}}, service, AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeProduction, PublicURL: "https://atalaya.example", Random: strings.NewReader("")})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://atalaya.example/api/v1/auth/me", nil)
	request.Header.Set("X-Request-ID", "attacker-controlled")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || response.Header().Get("X-Request-ID") != "unavailable" {
		t.Fatalf("response = %d headers=%#v", response.Code, response.Header())
	}
	for _, header := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "Permissions-Policy"} {
		if response.Header().Get(header) == "" {
			t.Errorf("missing %s", header)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("API headers = %#v", response.Header())
	}
	var body struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != "internal_error" || body.Error.RequestID != "unavailable" {
		t.Fatalf("body = %q err=%v", response.Body.String(), err)
	}
}
func (repository *httpAuthRepository) ChangePasswordAndReplaceSessions(context.Context, string, int, string, time.Time, auth.Session) error {
	return repository.changeError
}
func (*httpAuthRepository) ResetPasswordAndRevokeSessions(context.Context, string, time.Time) error {
	return nil
}

func TestAuthenticatedHandlerLoginSessionCSRFAndSecurityHeaders(t *testing.T) {
	hash, err := auth.HashPasswordWithRandom([]byte("correct horse battery staple"), bytes.NewReader(bytes.Repeat([]byte{1}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	repository := &httpAuthRepository{administrator: auth.Administrator{ID: "00000000-0000-4000-8000-000000000001", Username: "admin", PasswordHash: hash, Version: 1}}
	service, err := auth.NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	handler, err := NewAuthenticated(fstest.MapFS{"index.html": {Data: []byte("app")}}, service, AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeProduction, PublicURL: "https://atalaya.example", Clock: func() time.Time { return now }, Random: bytes.NewReader(bytes.Repeat([]byte{2}, 128))})
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest(http.MethodPost, "https://atalaya.example/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"correct horse battery staple"}`))
	login.Header.Set("Origin", "https://atalaya.example")
	login.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, login)
	if response.Code != http.StatusOK {
		t.Fatalf("login=%d %s", response.Code, response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	if cookie.Name != sessionCookie || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("cookie=%+v", cookie)
	}
	for header := range map[string]bool{"X-Request-ID": true, "Content-Security-Policy": true, "X-Content-Type-Options": true, "Referrer-Policy": true, "Permissions-Policy": true} {
		if response.Header().Get(header) == "" {
			t.Errorf("missing %s", header)
		}
	}
	csrfRequest := httptest.NewRequest(http.MethodGet, "https://atalaya.example/api/v1/auth/csrf", nil)
	csrfRequest.AddCookie(cookie)
	csrfResponse := httptest.NewRecorder()
	handler.ServeHTTP(csrfResponse, csrfRequest)
	if csrfResponse.Code != 200 {
		t.Fatalf("csrf=%d %s", csrfResponse.Code, csrfResponse.Body.String())
	}
	var body struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(csrfResponse.Body.Bytes(), &body); err != nil || body.CSRFToken == "" {
		t.Fatalf("csrf body=%q err=%v", csrfResponse.Body.String(), err)
	}
	logout := httptest.NewRequest(http.MethodPost, "https://atalaya.example/api/v1/auth/logout", nil)
	logout.AddCookie(cookie)
	logout.Header.Set("X-CSRF-Token", body.CSRFToken)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout=%d %s", logoutResponse.Code, logoutResponse.Body.String())
	}
}

func TestLoginRejectsOriginFetchMetadataAndStrictBody(t *testing.T) {
	service, err := auth.NewService(&httpAuthRepository{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(fstest.MapFS{"index.html": {Data: []byte("app")}}, service, AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeDevelopment, PublicURL: "http://127.0.0.1:8080", Random: bytes.NewReader(bytes.Repeat([]byte{4}, 128))})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, origin, site, body, content string
		status                            int
	}{{"origin", "http://evil", "", `{}`, "application/json", 403}, {"cross-site", "http://127.0.0.1:8080", "cross-site", `{}`, "application/json", 403}, {"unknown field", "http://127.0.0.1:8080", "", `{"username":"a","password":"b","extra":1}`, "application/json", 400}, {"content type", "http://127.0.0.1:8080", "", `{}`, "text/plain", 400}, {"oversized", "http://127.0.0.1:8080", "", `{"username":"a","password":"` + strings.Repeat("x", 4096) + `"}`, "application/json", 400}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/v1/auth/login", strings.NewReader(test.body))
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Sec-Fetch-Site", test.site)
			request.Header.Set("Content-Type", test.content)
			request.Header.Set("X-Request-ID", "attacker-controlled")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if response.Header().Get("X-Request-ID") == "attacker-controlled" {
				t.Fatal("incoming request ID was trusted")
			}
		})
	}
}

func TestCSRFFormulaMatchesContract(t *testing.T) {
	raw := bytes.Repeat([]byte{9}, 32)
	token := base64.RawURLEncoding.EncodeToString(raw)
	csrf, _ := auth.CSRFToken(token)
	digest := sha256.Sum256(raw)
	if csrf == base64.RawURLEncoding.EncodeToString(digest[:]) {
		t.Fatal("CSRF unexpectedly equals session hash")
	}
}

func TestProtectedFallbackAndProxyTrustBoundaries(t *testing.T) {
	service, err := auth.NewService(&httpAuthRepository{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(fstest.MapFS{"index.html": {Data: []byte("secret app")}}, service, AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeProduction, PublicURL: "https://atalaya.example", TrustedProxy: startupconfig.TrustedProxyTailscaleServe, Random: bytes.NewReader(bytes.Repeat([]byte{8}, 128))})
	if err != nil {
		t.Fatal(err)
	}
	ui := httptest.NewRecorder()
	handler.ServeHTTP(ui, httptest.NewRequest(http.MethodGet, "https://atalaya.example/hosts", nil))
	if ui.Code != http.StatusSeeOther || ui.Header().Get("Location") != "/login" {
		t.Fatalf("protected UI=%d location=%q", ui.Code, ui.Header().Get("Location"))
	}
	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "https://atalaya.example/api/v1/missing", nil))
	if apiResponse.Code != http.StatusNotFound || strings.Contains(apiResponse.Body.String(), "secret app") {
		t.Fatalf("missing API=%d %q", apiResponse.Code, apiResponse.Body.String())
	}
	application := &authAPI{config: AuthConfig{Mode: startupconfig.ModeProduction, TrustedProxy: startupconfig.TrustedProxyTailscaleServe}}
	spoofed := httptest.NewRequest(http.MethodGet, "https://atalaya.example/", nil)
	spoofed.RemoteAddr = "100.64.0.1:1234"
	spoofed.Header.Set("Tailscale-User-Login", "attacker@example.com")
	if metadata := application.metadata(spoofed); metadata.TailscaleLogin != nil || metadata.ProxyIdentityInformational {
		t.Fatalf("external proxy headers trusted: %+v", metadata)
	}
	trusted := httptest.NewRequest(http.MethodGet, "https://atalaya.example/", nil)
	trusted.RemoteAddr = "127.0.0.1:1234"
	trusted.Header.Set("Tailscale-User-Name", "=?UTF-8?Q?Mart=C3=ADn?=")
	metadata := application.metadata(trusted)
	if metadata.TailscaleName == nil || *metadata.TailscaleName != "Martín" || !metadata.ProxyIdentityInformational {
		t.Fatalf("trusted metadata=%+v", metadata)
	}
}

func TestDevelopmentLoopbackCookieOmitsSecure(t *testing.T) {
	hash, err := auth.HashPasswordWithRandom([]byte("correct horse battery staple"), bytes.NewReader(bytes.Repeat([]byte{5}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	repository := &httpAuthRepository{administrator: auth.Administrator{ID: "00000000-0000-4000-8000-000000000001", Username: "admin", PasswordHash: hash, Version: 1}}
	service, err := auth.NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(fstest.MapFS{"index.html": {Data: []byte("app")}}, service, AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeDevelopment, PublicURL: "http://127.0.0.1:8080", Random: bytes.NewReader(bytes.Repeat([]byte{6}, 64))})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"correct horse battery staple"}`))
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("login=%d %s", response.Code, response.Body.String())
	}
	if response.Result().Cookies()[0].Secure {
		t.Fatal("development loopback cookie unexpectedly Secure")
	}
}

func TestAuthenticatedConstructorRejectsInsecureTrustConfiguration(t *testing.T) {
	service, err := auth.NewService(&httpAuthRepository{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		config AuthConfig
	}{
		{name: "development", config: AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeDevelopment, PublicURL: "https://atalaya.example", TrustedProxy: startupconfig.TrustedProxyTailscaleServe}},
		{name: "http public origin", config: AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeProduction, PublicURL: "http://atalaya.example", TrustedProxy: startupconfig.TrustedProxyTailscaleServe}},
		{name: "non-loopback listener", config: AuthConfig{Listen: "0.0.0.0:8080", Mode: startupconfig.ModeProduction, PublicURL: "https://atalaya.example", TrustedProxy: startupconfig.TrustedProxyTailscaleServe}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewAuthenticated(fstest.MapFS{"index.html": {Data: []byte("app")}}, service, test.config); err == nil {
				t.Fatal("NewAuthenticated() accepted insecure trust configuration")
			}
		})
	}
}

func TestUnknownAPIRoutesReturnUniformJSONAndPreserveHEAD(t *testing.T) {
	service, err := auth.NewService(&httpAuthRepository{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(fstest.MapFS{"index.html": {Data: []byte("secret app")}}, service, AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeProduction, PublicURL: "https://atalaya.example", Random: bytes.NewReader(bytes.Repeat([]byte{7}, 128))})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/api", "/api/v1/missing"} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(method, "https://atalaya.example"+route, nil))
			if response.Code != http.StatusNotFound {
				t.Errorf("%s %s status = %d", method, route, response.Code)
			}
			if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s %s headers = %#v", method, route, response.Header())
			}
			if method == http.MethodHead {
				if response.Body.Len() != 0 {
					t.Errorf("HEAD %s body = %q", route, response.Body.String())
				}
				continue
			}
			var body struct {
				Error struct {
					Code      string `json:"code"`
					RequestID string `json:"request_id"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != "not_found" || body.Error.RequestID == "" {
				t.Errorf("%s %s body = %q, error = %v", method, route, response.Body.String(), err)
			}
		}
	}
}

func TestKnownAPIEndpointsRejectWrongMethodsWithUniformJSON(t *testing.T) {
	service, err := auth.NewService(&httpAuthRepository{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(fstest.MapFS{"index.html": {Data: []byte("secret app")}}, service, AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeProduction, PublicURL: "https://atalaya.example", Random: bytes.NewReader(bytes.Repeat([]byte{9}, 256))})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/auth/login"},
		{method: http.MethodPost, path: "/api/v1/auth/me"},
		{method: http.MethodPost, path: "/api/v1/auth/csrf"},
		{method: http.MethodGet, path: "/api/v1/auth/logout"},
		{method: http.MethodHead, path: "/api/v1/auth/change-password"},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(test.method, "https://atalaya.example"+test.path, nil))
		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s status = %d", test.method, test.path, response.Code)
		}
		if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Request-ID") == "" || response.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s %s headers = %#v", test.method, test.path, response.Header())
		}
		if test.method == http.MethodHead {
			if response.Body.Len() != 0 {
				t.Errorf("HEAD %s body = %q", test.path, response.Body.String())
			}
			continue
		}
		var body struct {
			Error struct {
				Code      string `json:"code"`
				RequestID string `json:"request_id"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != "method_not_allowed" || body.Error.RequestID == "" {
			t.Errorf("%s %s body = %q, error = %v", test.method, test.path, response.Body.String(), err)
		}
	}
}

func TestMetadataRejectsOversizedTailscaleIdentity(t *testing.T) {
	application := &authAPI{config: AuthConfig{Mode: startupconfig.ModeProduction, TrustedProxy: startupconfig.TrustedProxyTailscaleServe}}
	request := httptest.NewRequest(http.MethodGet, "https://atalaya.example/api/v1/auth/me", nil)
	request.RemoteAddr = "127.0.0.1:54321"
	request.Header.Set("Tailscale-User-Login", strings.Repeat("a", maxProxyIdentityBytes+1))
	request.Header.Set("Tailscale-User-Name", strings.Repeat("b", maxProxyIdentityBytes))

	metadata := application.metadata(request)
	if metadata.TailscaleLogin != nil {
		t.Fatalf("oversized login persisted: %q", *metadata.TailscaleLogin)
	}
	if metadata.TailscaleName == nil || len(*metadata.TailscaleName) != maxProxyIdentityBytes {
		t.Fatalf("boundary-length name rejected: %#v", metadata.TailscaleName)
	}
}

func TestChangePasswordClassifiesDomainRaceAndInternalErrors(t *testing.T) {
	tests := []struct {
		name            string
		current         string
		newPassword     string
		validateAfter   int
		validateError   error
		changeError     error
		wantStatus      int
		wantCode        string
		wantClearCookie bool
	}{
		{name: "current password", current: "incorrect password value", newPassword: "new correct horse battery staple", wantStatus: http.StatusForbidden, wantCode: "current_password_invalid"},
		{name: "new password semantics", current: "correct horse battery staple", newPassword: "short", wantStatus: http.StatusUnprocessableEntity, wantCode: "validation_failed"},
		{name: "session expired race", current: "correct horse battery staple", newPassword: "new correct horse battery staple", validateAfter: 1, validateError: auth.ErrSessionExpired, wantStatus: http.StatusUnauthorized, wantCode: "session_expired", wantClearCookie: true},
		{name: "repository failure", current: "correct horse battery staple", newPassword: "new correct horse battery staple", changeError: errors.New("sqlite unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hash, err := auth.HashPasswordWithRandom([]byte("correct horse battery staple"), bytes.NewReader(bytes.Repeat([]byte{1}, 16)))
			if err != nil {
				t.Fatal(err)
			}
			repository := &httpAuthRepository{administrator: auth.Administrator{ID: "00000000-0000-4000-8000-000000000001", Username: "admin", PasswordHash: hash, Version: 1}, validateAfter: test.validateAfter, validateError: test.validateError, changeError: test.changeError}
			service, err := auth.NewService(repository)
			if err != nil {
				t.Fatal(err)
			}
			handler, err := NewAuthenticated(fstest.MapFS{"index.html": {Data: []byte("app")}}, service, AuthConfig{Listen: "127.0.0.1:8080", Mode: startupconfig.ModeProduction, PublicURL: "https://atalaya.example", Random: bytes.NewReader(bytes.Repeat([]byte{3}, 512))})
			if err != nil {
				t.Fatal(err)
			}
			login := httptest.NewRequest(http.MethodPost, "https://atalaya.example/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"correct horse battery staple"}`))
			login.Header.Set("Origin", "https://atalaya.example")
			login.Header.Set("Content-Type", "application/json")
			loginResponse := httptest.NewRecorder()
			handler.ServeHTTP(loginResponse, login)
			if loginResponse.Code != http.StatusOK {
				t.Fatalf("login status = %d: %s", loginResponse.Code, loginResponse.Body.String())
			}
			cookie := loginResponse.Result().Cookies()[0]
			csrf, err := auth.CSRFToken(cookie.Value)
			if err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"current_password":%q,"new_password":%q}`, test.current, test.newPassword)
			request := httptest.NewRequest(http.MethodPost, "https://atalaya.example/api/v1/auth/change-password", strings.NewReader(body))
			request.AddCookie(cookie)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", csrf)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("change password = %d %s", response.Code, response.Body.String())
			}
			cleared := false
			for _, responseCookie := range response.Result().Cookies() {
				cleared = cleared || responseCookie.Name == sessionCookie && responseCookie.MaxAge < 0
			}
			if cleared != test.wantClearCookie {
				t.Fatalf("cookie cleared = %v, want %v", cleared, test.wantClearCookie)
			}
		})
	}
}
