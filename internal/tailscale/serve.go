// Package tailscale hides the version-sensitive Tailscale Serve CLI behind a
// small capability-checked adapter.
package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	DashboardPort   = 443
	AgentPort       = 9474
	DashboardTarget = "http://127.0.0.1:8080"
	AgentTarget     = "http://127.0.0.1:9474"
)

// VersionMatrix is published with a release. Versions outside it fail before
// any Serve mutation is attempted.
type VersionMatrix struct {
	OldestSupported string
	NewestSupported string
}

var DefaultVersionMatrix = VersionMatrix{OldestSupported: "1.52.0", NewestSupported: "1.99.99"}

type Command interface {
	Run(context.Context, ...string) ([]byte, error)
}

type execCommand struct{ path string }

func (command execCommand) Run(ctx context.Context, args ...string) ([]byte, error) {
	process := exec.CommandContext(ctx, command.path, args...)
	output, err := process.CombinedOutput()
	if err != nil {
		return nil, err
	}
	return output, nil
}

// Runner is useful to tests and to callers embedding the adapter.
type Runner func(context.Context, ...string) ([]byte, error)

func (runner Runner) Run(ctx context.Context, args ...string) ([]byte, error) {
	return runner(ctx, args...)
}

// Adapter validates Tailscale and configures only the two private HTTPS routes.
type Adapter struct {
	Command       Command
	Architecture  string
	Matrix        VersionMatrix
	TailscalePath string
	// DesiredRoutes contains HTTPS port to local proxy mappings. A nil map
	// preserves the default server-plus-local-agent configuration.
	DesiredRoutes map[int]string
}

// Result describes the verified Serve configuration.
type Result struct {
	Version string
	DNSName string
	Routes  map[int]string
}

var versionPattern = regexp.MustCompile(`(?:^|\s)([0-9]+\.[0-9]+\.[0-9]+)(?:\s|$)`)

// New returns the production adapter. It does not run a command until
// Configure is called.
func New() Adapter {
	return Adapter{Architecture: runtime.GOARCH, Matrix: DefaultVersionMatrix, TailscalePath: "tailscale"}
}

// ConfigureRoutes validates and configures the supplied exact HTTPS routes.
// It is used by role-specific installation paths so an agent-only host does
// not claim the server's dashboard port.
func (adapter Adapter) ConfigureRoutes(ctx context.Context, routes map[int]string) (Result, error) {
	adapter.DesiredRoutes = cloneRoutes(routes)
	return adapter.Configure(ctx)
}

// Configure validates architecture, version, connection and DNS identity,
// then adds missing exact routes and verifies the final status. It never calls
// `serve reset`, `funnel`, or any public-listener fallback.
func (adapter Adapter) Configure(ctx context.Context) (Result, error) {
	if adapter.Command == nil {
		path := adapter.TailscalePath
		if path == "" {
			path = "tailscale"
		}
		adapter.Command = execCommand{path: path}
	}
	desired := adapter.DesiredRoutes
	if len(desired) == 0 {
		desired = map[int]string{DashboardPort: DashboardTarget, AgentPort: AgentTarget}
	}
	capability := requiredCapability(desired)
	architecture := adapter.Architecture
	if architecture == "" {
		architecture = runtime.GOARCH
	}
	if architecture != "amd64" && architecture != "arm64" {
		return Result{}, fmt.Errorf("unsupported architecture %q for Tailscale Serve; required capability %s is unavailable", architecture, capability)
	}
	matrix := adapter.Matrix
	if matrix.OldestSupported == "" {
		matrix = DefaultVersionMatrix
	}
	versionOutput, err := adapter.Command.Run(ctx, "version")
	if err != nil {
		return Result{}, fmt.Errorf("tailscale version unavailable; required capability %s is unavailable", capability)
	}
	version, err := parseVersion(string(versionOutput))
	if err != nil {
		return Result{}, fmt.Errorf("tailscale version is unsupported (%q); required capability %s is unavailable", strings.TrimSpace(string(versionOutput)), capability)
	}
	if !inVersionMatrix(version, matrix) {
		return Result{}, fmt.Errorf("tailscale version %s is outside the supported matrix %s..%s; required capability %s is unavailable", version, matrix.OldestSupported, matrix.NewestSupported, capability)
	}
	statusOutput, err := adapter.Command.Run(ctx, "status", "--json")
	if err != nil {
		return Result{}, fmt.Errorf("tailscale connection status unavailable; required capability %s is unavailable", capability)
	}
	dnsName, backendState, err := parseStatus(statusOutput)
	if err != nil || backendState != "Running" || dnsName == "" {
		return Result{}, fmt.Errorf("tailscale is not connected or Self.DNSName is unavailable; required capability %s is unavailable", capability)
	}
	serveOutput, err := adapter.Command.Run(ctx, "serve", "status", "--json")
	if err != nil {
		return Result{}, fmt.Errorf("tailscale Serve status unavailable for version %s; required capability %s is unavailable", version, capability)
	}
	routes, err := parseRoutes(serveOutput)
	if err != nil {
		return Result{}, fmt.Errorf("tailscale Serve status is incompatible for version %s; required capability %s is unavailable", version, capability)
	}
	for _, port := range sortedPorts(desired) {
		target := desired[port]
		occupied, exact := routeForPort(routes, dnsName, port, target)
		if occupied && !exact {
			return Result{}, fmt.Errorf("tailscale Serve conflict on HTTPS %d: required target %s; capability has a different endpoint; version %s", port, target, version)
		}
	}
	for _, port := range sortedPorts(desired) {
		target := desired[port]
		_, exact := routeForPort(routes, dnsName, port, target)
		if exact {
			continue
		}
		if _, err := adapter.Command.Run(ctx, "serve", "--bg", fmt.Sprintf("--https=%d", port), target); err != nil {
			return Result{}, fmt.Errorf("configure tailscale Serve HTTPS %d for version %s; capability %s is unavailable", port, version, capability)
		}
	}
	verifiedOutput, err := adapter.Command.Run(ctx, "serve", "status", "--json")
	if err != nil {
		return Result{}, fmt.Errorf("verify tailscale Serve routes for version %s; capability %s is unavailable", version, capability)
	}
	verified, err := parseRoutes(verifiedOutput)
	if err != nil {
		return Result{}, fmt.Errorf("verify tailscale Serve routes for version %s; capability %s is unavailable", version, capability)
	}
	resultRoutes := make(map[int]string, len(desired))
	for _, port := range sortedPorts(desired) {
		target := desired[port]
		_, exact := routeForPort(verified, dnsName, port, target)
		if !exact {
			return Result{}, fmt.Errorf("tailscale Serve route HTTPS %d was not verified for version %s; capability %s is unavailable", port, version, capability)
		}
		resultRoutes[port] = target
	}
	return Result{Version: version, DNSName: dnsName, Routes: resultRoutes}, nil
}

type semver struct{ major, minor, patch int }

func parseVersion(value string) (string, error) {
	match := versionPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return "", errors.New("version not found")
	}
	parts := strings.Split(match[1], ".")
	if len(parts) != 3 {
		return "", errors.New("invalid version")
	}
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return "", errors.New("invalid version")
		}
	}
	return match[1], nil
}

func parseSemver(value string) (semver, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return semver{}, errors.New("invalid semver")
	}
	values := [3]int{}
	for index, part := range parts {
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return semver{}, errors.New("invalid semver")
		}
		values[index] = parsed
	}
	return semver{major: values[0], minor: values[1], patch: values[2]}, nil
}

func compareVersion(left, right semver) int {
	if left.major != right.major {
		if left.major < right.major {
			return -1
		}
		return 1
	}
	if left.minor != right.minor {
		if left.minor < right.minor {
			return -1
		}
		return 1
	}
	if left.patch < right.patch {
		return -1
	}
	if left.patch > right.patch {
		return 1
	}
	return 0
}

func inVersionMatrix(value string, matrix VersionMatrix) bool {
	current, currentErr := parseSemver(value)
	oldest, oldestErr := parseSemver(matrix.OldestSupported)
	newest, newestErr := parseSemver(matrix.NewestSupported)
	return currentErr == nil && oldestErr == nil && newestErr == nil && compareVersion(current, oldest) >= 0 && compareVersion(current, newest) <= 0
}

func parseStatus(data []byte) (string, string, error) {
	var value struct {
		BackendState string `json:"BackendState"`
		Self         struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return "", "", err
	}
	return strings.TrimSuffix(value.Self.DNSName, "."), value.BackendState, nil
}

type serveRoute struct {
	port   int
	scheme string
	host   string
	path   string
	proxy  string
}

func parseRoutes(data []byte) ([]serveRoute, error) {
	var value struct {
		TCP map[string]struct {
			HTTPS bool `json:"HTTPS"`
			HTTP  bool `json:"HTTP"`
		} `json:"TCP"`
		Web map[string]struct {
			Handlers map[string]struct {
				Proxy string `json:"Proxy"`
			} `json:"Handlers"`
		} `json:"Web"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	routes := make([]serveRoute, 0, len(value.Web))
	for address, webServer := range value.Web {
		parsedAddress, err := parseRouteAddress(address)
		if err != nil {
			return nil, err
		}
		if parsedAddress.scheme == "" {
			protocol, ok := value.TCP[strconv.Itoa(parsedAddress.port)]
			if !ok {
				return nil, errors.New("missing protocol for web route")
			}
			switch {
			case protocol.HTTPS:
				parsedAddress.scheme = "https"
			case protocol.HTTP:
				parsedAddress.scheme = "http"
			default:
				return nil, errors.New("unsupported protocol for web route")
			}
		}
		if webServer.Handlers == nil {
			return nil, errors.New("missing web handlers")
		}
		root, ok := webServer.Handlers["/"]
		if !ok {
			return nil, errors.New("missing root web handler")
		}
		if root.Proxy == "" {
			return nil, errors.New("web route is not a proxy")
		}
		parsedAddress.proxy = root.Proxy
		routes = append(routes, parsedAddress)
	}
	return routes, nil
}

func parseRouteAddress(address string) (serveRoute, error) {
	if !strings.Contains(address, "://") {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host == "" || port == "" {
			return serveRoute{}, errors.New("invalid route address")
		}
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return serveRoute{}, errors.New("invalid route port")
		}
		return serveRoute{port: value, host: normalizeDNSName(host), path: "/"}, nil
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.ForceQuery || parsed.Opaque != "" {
		return serveRoute{}, errors.New("invalid route address")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return serveRoute{}, errors.New("invalid route scheme")
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return serveRoute{}, errors.New("invalid route port")
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return serveRoute{
		port:   value,
		scheme: scheme,
		host:   normalizeDNSName(parsed.Hostname()),
		path:   path,
	}, nil
}

func routeForPort(routes []serveRoute, dnsName string, port int, target string) (occupied, exact bool) {
	for _, route := range routes {
		if route.port != port {
			continue
		}
		occupied = true
		if route.scheme == "https" && route.host == normalizeDNSName(dnsName) && route.path == "/" && route.proxy == target {
			exact = true
			continue
		}
		return occupied, false
	}
	return occupied, exact
}

func requiredCapability(routes map[int]string) string {
	_, dashboard := routes[DashboardPort]
	_, agent := routes[AgentPort]
	switch {
	case dashboard && agent:
		return "serve_https_443_9474"
	case dashboard:
		return "serve_https_443"
	case agent:
		return "serve_https_9474"
	default:
		return "serve_https"
	}
}

func cloneRoutes(routes map[int]string) map[int]string {
	if routes == nil {
		return nil
	}
	clone := make(map[int]string, len(routes))
	for port, target := range routes {
		clone[port] = target
	}
	return clone
}

func sortedPorts(routes map[int]string) []int {
	ports := make([]int, 0, len(routes))
	for port := range routes {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

func normalizeDNSName(value string) string {
	return strings.ToLower(strings.TrimSuffix(value, "."))
}

/*
Keep the route parser strict about the endpoint identity. In particular,
`http://host:443`, another hostname, and another path must never satisfy an
HTTPS root route merely because they share its numeric port.
*/
func httpsPort(address string) (int, error) {
	route, err := parseRouteAddress(address)
	if err != nil {
		return 0, err
	}
	return route.port, nil
}
