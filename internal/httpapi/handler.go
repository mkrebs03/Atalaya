// Package httpapi composes the public health endpoint and embedded SPA delivery.
package httpapi

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"
)

const (
	noCache       = "no-cache"
	immutableYear = "public, max-age=31536000, immutable"
)

var hashedAsset = regexp.MustCompile(`-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`)

// New builds the Phase 0 HTTP handler and verifies that the embedded index exists.
func New(assets fs.FS) (http.Handler, error) {
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read embedded index: %w", err)
	}

	application := &applicationHandler{
		assets:     assets,
		fileServer: http.FileServer(http.FS(assets)),
		index:      index,
	}

	return application, nil
}

type applicationHandler struct {
	assets     fs.FS
	fileServer http.Handler
	index      []byte
}

func health(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(`{"status":"ok"}`))
}

func (handler *applicationHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if hasTraversalSegment(request.URL.Path) {
		writeNotFound(response, request)
		return
	}
	if request.URL.Path == "/healthz" {
		health(response, request)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/healthz/") {
		writeNotFound(response, request)
		return
	}
	if isAPIPath(request.URL.Path) {
		writeNotFound(response, request)
		return
	}
	assetPath := isAssetPath(request.URL.Path)
	if !assetPath && looksLikeFilePath(request.URL.Path) {
		writeNotFound(response, request)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if assetPath {
		handler.serveAsset(response, request)
		return
	}
	handler.serveIndex(response, request)
}

func (handler *applicationHandler) serveAsset(response http.ResponseWriter, request *http.Request) {
	name, valid := assetName(request.URL.Path)
	if !valid {
		writeNotFound(response, request)
		return
	}

	info, err := fs.Stat(handler.assets, name)
	if err != nil || info.IsDir() {
		writeNotFound(response, request)
		return
	}

	if hashedAsset.MatchString(path.Base(name)) {
		response.Header().Set("Cache-Control", immutableYear)
	} else {
		response.Header().Set("Cache-Control", noCache)
	}
	handler.fileServer.ServeHTTP(response, request)
}

func (handler *applicationHandler) serveIndex(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", noCache)
	response.Header().Set("Content-Type", mime.TypeByExtension(".html"))
	response.Header().Set("Content-Length", fmt.Sprintf("%d", len(handler.index)))
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	_, _ = response.Write(handler.index) //nolint:gosec // handler.index is the build-time embedded SPA index.html, not derived from any request
}

func isAPIPath(requestPath string) bool {
	return requestPath == "/api" || strings.HasPrefix(requestPath, "/api/")
}

func isAssetPath(requestPath string) bool {
	return requestPath == "/assets" || strings.HasPrefix(requestPath, "/assets/")
}

func assetName(requestPath string) (string, bool) {
	if hasTraversalSegment(requestPath) {
		return "", false
	}

	cleaned := path.Clean(requestPath)
	if cleaned != requestPath || !strings.HasPrefix(cleaned, "/assets/") {
		return "", false
	}
	return strings.TrimPrefix(cleaned, "/"), true
}

func hasTraversalSegment(requestPath string) bool {
	for _, segment := range strings.Split(requestPath, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

func looksLikeFilePath(requestPath string) bool {
	base := path.Base(path.Clean(requestPath))
	return base != "." && path.Ext(base) != ""
}

func writeNotFound(response http.ResponseWriter, request *http.Request) {
	body := []byte("not found\n")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusNotFound)
	if request.Method == http.MethodHead {
		return
	}
	_, _ = response.Write(body)
}
