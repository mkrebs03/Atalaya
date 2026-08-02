package tailscale

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestServeCompatibilityWithInstalledBinary is enabled by the release
// workflow. Tailscale Serve needs an authenticated node to mutate real routes,
// so the test uses the installed release binary for version detection and a
// deterministic connected/Serve response for the adapter's full route
// validation and command-shape checks.
func TestServeCompatibilityWithInstalledBinary(t *testing.T) {
	binary := os.Getenv("ATALAYA_TAILSCALE_BIN")
	if binary == "" {
		t.Skip("ATALAYA_TAILSCALE_BIN is not set")
	}
	versionOutput, err := exec.CommandContext(context.Background(), binary, "version").Output()
	if err != nil {
		t.Fatalf("tailscale version: %v", err)
	}
	version, err := parseVersion(string(versionOutput))
	if err != nil {
		t.Fatalf("parse installed tailscale version %q: %v", strings.TrimSpace(string(versionOutput)), err)
	}

	serveStatusCalls := 0
	runner := Runner(func(_ context.Context, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "version":
			return versionOutput, nil
		case "status --json":
			return connectedStatus(), nil
		case "serve status --json":
			serveStatusCalls++
			if serveStatusCalls == 1 {
				return emptyServeStatus(), nil
			}
			return exactServeStatus(), nil
		case "serve --bg --https=443 http://127.0.0.1:8080", "serve --bg --https=9474 http://127.0.0.1:9474":
			return nil, nil
		default:
			return nil, errors.New("unexpected Tailscale command")
		}
	})
	result, err := (Adapter{Command: runner, Architecture: "amd64", Matrix: DefaultVersionMatrix}).Configure(context.Background())
	if err != nil {
		t.Fatalf("configure with Tailscale %s: %v", version, err)
	}
	if result.Routes[DashboardPort] != DashboardTarget || result.Routes[AgentPort] != AgentTarget {
		t.Fatalf("verified routes = %+v", result.Routes)
	}
}
