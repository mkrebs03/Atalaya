package tailscale

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func connectedStatus() []byte {
	return []byte("{\"BackendState\":\"Running\",\"Self\":{\"DNSName\":\"oracle.example.ts.net.\"}}")
}

func emptyServeStatus() []byte {
	return []byte("{\"Web\":{}}")
}

func exactServeStatus() []byte {
	return []byte("{\"Web\":{\"https://oracle.example.ts.net/\":{\"Handlers\":{\"/\":{\"Proxy\":\"http://127.0.0.1:8080\"}}},\"https://oracle.example.ts.net:9474/\":{\"Handlers\":{\"/\":{\"Proxy\":\"http://127.0.0.1:9474\"}}}}}")
}

func TestConfigureRequiresSupportedVersionAndNeverResets(t *testing.T) {
	var calls [][]string
	runner := Runner(func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		switch strings.Join(args, " ") {
		case "version":
			return []byte("1.51.0\n"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	})
	adapter := Adapter{Command: runner, Architecture: "amd64", Matrix: DefaultVersionMatrix}
	if _, err := adapter.Configure(context.Background()); err == nil || !strings.Contains(err.Error(), "1.51.0") {
		t.Fatalf("Configure() error = %v", err)
	}
	for _, call := range calls {
		if strings.Contains(strings.Join(call, " "), "reset") {
			t.Fatal("adapter invoked serve reset")
		}
	}
}

func TestConfigureDetectsConflictBeforeMutatingServe(t *testing.T) {
	var calls [][]string
	runner := Runner(func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		switch strings.Join(args, " ") {
		case "version":
			return []byte("1.86.2\n"), nil
		case "status --json":
			return connectedStatus(), nil
		case "serve status --json":
			return []byte("{\"Web\":{\"https://oracle.example.ts.net/\":{\"Handlers\":{\"/\":{\"Proxy\":\"http://127.0.0.1:9999\"}}}}}"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	})
	adapter := Adapter{Command: runner, Architecture: "amd64", Matrix: DefaultVersionMatrix}
	if _, err := adapter.Configure(context.Background()); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("Configure() error = %v", err)
	}
	for _, call := range calls {
		if call[0] == "serve" && len(call) > 1 && call[1] != "status" {
			t.Fatalf("Serve was mutated after conflict: %#v", call)
		}
	}
}

func TestConfigureAddsBothRoutesAndVerifiesThem(t *testing.T) {
	var calls [][]string
	serveStatusCalls := 0
	runner := Runner(func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		switch strings.Join(args, " ") {
		case "version":
			return []byte("1.86.2\n"), nil
		case "status --json":
			return connectedStatus(), nil
		case "serve status --json":
			serveStatusCalls++
			if serveStatusCalls == 1 {
				return emptyServeStatus(), nil
			}
			return exactServeStatus(), nil
		case "serve --https=443 http://127.0.0.1:8080", "serve --https=9474 http://127.0.0.1:9474":
			return nil, nil
		default:
			return nil, errors.New("unexpected command")
		}
	})
	result, err := (Adapter{Command: runner, Architecture: "arm64", Matrix: DefaultVersionMatrix}).Configure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.DNSName != "oracle.example.ts.net" || result.Routes[443] != DashboardTarget || result.Routes[9474] != AgentTarget {
		t.Fatalf("result = %+v", result)
	}
	if serveStatusCalls != 2 {
		t.Fatalf("serve status calls = %d, want 2", serveStatusCalls)
	}
}

func TestConfigureRoleSpecificRoutesDoesNotClaimDashboard(t *testing.T) {
	var calls [][]string
	runner := Runner(func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		switch strings.Join(args, " ") {
		case "version":
			return []byte("1.86.2\n"), nil
		case "status --json":
			return connectedStatus(), nil
		case "serve status --json":
			return []byte("{\"Web\":{\"https://oracle.example.ts.net:9474/\":{\"Handlers\":{\"/\":{\"Proxy\":\"http://127.0.0.1:9474\"}}}}}"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	})
	result, err := (Adapter{Command: runner, Architecture: "amd64", Matrix: DefaultVersionMatrix}).ConfigureRoutes(context.Background(), map[int]string{AgentPort: AgentTarget})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Routes) != 1 || result.Routes[AgentPort] != AgentTarget {
		t.Fatalf("routes = %+v", result.Routes)
	}
	for _, call := range calls {
		if strings.Join(call, " ") == "serve --https=443 http://127.0.0.1:8080" {
			t.Fatal("agent route configuration claimed dashboard port")
		}
	}
}

func TestConfigureRequiresExactHTTPSHostAndPath(t *testing.T) {
	cases := map[string]string{
		"http scheme": "http://oracle.example.ts.net:443/",
		"wrong host":  "https://other.example.ts.net:443/",
		"wrong path":  "https://oracle.example.ts.net:443/other",
	}
	for name, address := range cases {
		t.Run(name, func(t *testing.T) {
			runner := Runner(func(_ context.Context, args ...string) ([]byte, error) {
				switch strings.Join(args, " ") {
				case "version":
					return []byte("1.86.2\n"), nil
				case "status --json":
					return connectedStatus(), nil
				case "serve status --json":
					return []byte("{\"Web\":{\"" + address + "\":{\"Handlers\":{\"/\":{\"Proxy\":\"http://127.0.0.1:8080\"}}}}}"), nil
				default:
					return nil, errors.New("unexpected mutation")
				}
			})
			if _, err := (Adapter{Command: runner, Architecture: "amd64", Matrix: DefaultVersionMatrix}).Configure(context.Background()); err == nil || !strings.Contains(err.Error(), "conflict") {
				t.Fatalf("Configure() error = %v", err)
			}
		})
	}
}
