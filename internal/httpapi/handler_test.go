package httpapi

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var testAssets fs.FS = fstest.MapFS{
	"index.html":               {Data: []byte("<!doctype html><title>Atalaya</title><main>bootstrap</main>")},
	"assets/index-AbCd1234.js": {Data: []byte("console.log('atalaya')")},
	"assets/manifest.json":     {Data: []byte(`{"phase":0}`)},
}

func TestHealthContract(t *testing.T) {
	handler := newTestHandler(t)
	response := request(t, handler, http.MethodGet, "/healthz")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Body.String(); got != `{"status":"ok"}` {
		t.Fatalf("body = %q", got)
	}
}

func TestHealthRejectsEveryMethodExceptGet(t *testing.T) {
	handler := newTestHandler(t)
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete} {
		response := request(t, handler, method, "/healthz")
		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s status = %d, want %d", method, response.Code, http.StatusMethodNotAllowed)
		}
		if got := response.Header().Get("Allow"); got != http.MethodGet {
			t.Errorf("%s Allow = %q, want GET", method, got)
		}
	}
}

func TestHealthSubpathsDoNotReceiveTheSPA(t *testing.T) {
	handler := newTestHandler(t)
	response := request(t, handler, http.MethodGet, "/healthz/details")

	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "bootstrap") {
		t.Fatalf("GET health subpath = (%d, %q), want non-HTML 404", response.Code, response.Body.String())
	}
}

func TestHashedAssetsAreImmutable(t *testing.T) {
	handler := newTestHandler(t)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := request(t, handler, method, "/assets/index-AbCd1234.js")

		if response.Code != http.StatusOK {
			t.Errorf("%s status = %d, want %d", method, response.Code, http.StatusOK)
		}
		if got := response.Header().Get("Cache-Control"); got != immutableYear {
			t.Errorf("%s Cache-Control = %q, want %q", method, got, immutableYear)
		}
		if got := response.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
			t.Errorf("%s Content-Type = %q, want JavaScript", method, got)
		}
		if method == http.MethodHead && response.Body.Len() != 0 {
			t.Errorf("HEAD body = %q, want empty", response.Body.String())
		}
	}
}

func TestNonHashedAssetsAreNotCachedImmutably(t *testing.T) {
	handler := newTestHandler(t)
	response := request(t, handler, http.MethodGet, "/assets/manifest.json")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Cache-Control"); got != noCache {
		t.Fatalf("Cache-Control = %q, want %q", got, noCache)
	}
}

func TestSPAFallbackOnlyHandlesSafeUIRoutes(t *testing.T) {
	handler := newTestHandler(t)

	for _, route := range []string{"/", "/hosts/oracle", "/hosts/oracle/", "/ruta-desconocida"} {
		response := request(t, handler, http.MethodGet, route)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "bootstrap") {
			t.Errorf("GET %s = (%d, %q), want embedded index", route, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Cache-Control"); got != noCache {
			t.Errorf("GET %s Cache-Control = %q, want %q", route, got, noCache)
		}
	}

	head := request(t, handler, http.MethodHead, "/hosts/oracle")
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD UI route = (%d, %q), want 200 with empty body", head.Code, head.Body.String())
	}
	if got := head.Header().Get("Cache-Control"); got != noCache {
		t.Fatalf("HEAD UI Cache-Control = %q, want %q", got, noCache)
	}
	if got := head.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("HEAD UI Content-Type = %q, want HTML", got)
	}

	unsafe := request(t, handler, http.MethodPost, "/hosts/oracle")
	if unsafe.Code != http.StatusMethodNotAllowed || strings.Contains(unsafe.Body.String(), "bootstrap") {
		t.Fatalf("POST UI route = (%d, %q), want non-HTML 405", unsafe.Code, unsafe.Body.String())
	}
}

func TestTraversalSegmentsAreRejectedBeforeSPAFallback(t *testing.T) {
	handler := newTestHandler(t)

	for _, route := range []string{
		"/foo/../bar",
		"/foo/%2e%2e/bar",
		"/foo/./bar",
		"/foo/%2e/bar",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			response := request(t, handler, method, route)
			if response.Code != http.StatusNotFound {
				t.Errorf("%s %s status = %d, want 404", method, route, response.Code)
			}
			if got := response.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
				t.Errorf("%s %s Content-Type = %q, want text/plain", method, route, got)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("%s %s Cache-Control = %q, want no-store", method, route, got)
			}

			wantBody := "not found\n"
			if method == http.MethodHead {
				wantBody = ""
			}
			if got := response.Body.String(); got != wantBody {
				t.Errorf("%s %s body = %q, want %q", method, route, got, wantBody)
			}
		}
	}
}

func TestAPIAndFileLikePathsNeverReceiveHTML(t *testing.T) {
	handler := newTestHandler(t)

	for _, route := range []string{
		"/api",
		"/api/v1/hosts",
		"/assets/missing-AbCd1234.js",
		"/assets",
		"/favicon.ico",
		"/app.js",
		"/site.webmanifest",
		"/route.with-dot",
		"/index.html",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			response := request(t, handler, method, route)
			if response.Code != http.StatusNotFound {
				t.Errorf("%s %s status = %d, want 404", method, route, response.Code)
			}
			if strings.Contains(response.Body.String(), "bootstrap") {
				t.Errorf("%s %s received SPA HTML", method, route)
			}
			if got := response.Header().Get("Content-Type"); strings.HasPrefix(got, "text/html") {
				t.Errorf("%s %s Content-Type = %q, want non-HTML", method, route, got)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("%s %s Cache-Control = %q, want no-store", method, route, got)
			}
			if method == http.MethodHead && response.Body.Len() != 0 {
				t.Errorf("HEAD %s body = %q, want empty", route, response.Body.String())
			}
		}
	}
}

func TestFileLikePathsOutsideAssetsReturnNotFoundForUnsafeMethods(t *testing.T) {
	handler := newTestHandler(t)

	for _, route := range []string{"/favicon.ico", "/app.js", "/site.webmanifest"} {
		response := request(t, handler, http.MethodPost, route)
		if response.Code != http.StatusNotFound {
			t.Errorf("POST %s status = %d, want 404", route, response.Code)
		}
		if got := response.Header().Get("Content-Type"); strings.HasPrefix(got, "text/html") {
			t.Errorf("POST %s Content-Type = %q, want non-HTML", route, got)
		}
	}
}

func TestAssetTraversalIsRejectedWithoutHTML(t *testing.T) {
	handler := newTestHandler(t)

	for _, route := range []string{
		"/assets/../index.html",
		"/assets/%2e%2e/index.html",
		"/assets//index-AbCd1234.js",
	} {
		response := request(t, handler, http.MethodGet, route)
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", route, response.Code)
		}
		if got := response.Header().Get("Content-Type"); strings.HasPrefix(got, "text/html") {
			t.Errorf("GET %s Content-Type = %q, want non-HTML", route, got)
		}
		if strings.Contains(response.Body.String(), "bootstrap") {
			t.Errorf("GET %s received SPA HTML", route)
		}
	}
}

func TestNewRequiresEmbeddedIndex(t *testing.T) {
	_, err := New(fstest.MapFS{})
	if err == nil {
		t.Fatal("New() error = nil, want missing index failure")
	}
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	handler, err := New(testAssets)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func request(t *testing.T, handler http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, target, nil))
	return recorder
}
