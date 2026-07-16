GO ?= go
NPM ?= npm
VERSION ?= dev

GO_PACKAGES := ./cmd/... ./internal/... ./web
WEB_DEPS_STAMP := web/node_modules/.atalaya-dependencies
LDFLAGS := -s -w -X github.com/mkrebs03/atalaya/internal/buildinfo.Version=$(VERSION)

.PHONY: test lint build test-e2e verify-cross-deps install-e2e-browser dev-server web-dev

$(WEB_DEPS_STAMP): web/package.json web/package-lock.json .tool-versions
	cd web && $(NPM) ci
	touch $(WEB_DEPS_STAMP)

test: $(WEB_DEPS_STAMP)
	cd web && $(NPM) run test:unit
	cd web && $(NPM) run build
	CGO_ENABLED=0 $(GO) test $(GO_PACKAGES)

lint: $(WEB_DEPS_STAMP)
	cd web && $(NPM) run format:check
	cd web && $(NPM) run lint
	cd web && $(NPM) run type-check
	cd web && $(NPM) run build
	@files="$$($(GO) list -f '{{range .GoFiles}}{{$$.Dir}}/{{.}} {{end}}{{range .TestGoFiles}}{{$$.Dir}}/{{.}} {{end}}{{range .XTestGoFiles}}{{$$.Dir}}/{{.}} {{end}}' ./cmd/... ./internal/... ./web)"; \
		unformatted="$$($(GO)fmt -l $$files)"; \
		test -z "$$unformatted" || { echo "Go files require gofmt:"; echo "$$unformatted"; exit 1; }
	CGO_ENABLED=0 $(GO) vet $(GO_PACKAGES)
	$(GO) mod tidy -diff
	$(GO) mod verify

build: $(WEB_DEPS_STAMP) verify-cross-deps
	cd web && $(NPM) run build
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o build/atalaya-linux-amd64 ./cmd/atalaya
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o build/atalaya-linux-arm64 ./cmd/atalaya

test-e2e: $(WEB_DEPS_STAMP)
	cd web && $(NPM) run build
	mkdir -p build
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o build/atalaya ./cmd/atalaya
	cd web && $(NPM) run test:e2e

verify-cross-deps:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) test -exec=true -run '^$$' ./internal/server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) test -exec=true -run '^$$' ./internal/server

install-e2e-browser: $(WEB_DEPS_STAMP)
	cd web && $(NPM) exec playwright install chromium

dev-server: $(WEB_DEPS_STAMP)
	cd web && $(NPM) run build
	$(GO) run ./cmd/atalaya server --mode development --listen 127.0.0.1:8080 --public-url http://127.0.0.1:5173

web-dev: $(WEB_DEPS_STAMP)
	cd web && $(NPM) run dev
