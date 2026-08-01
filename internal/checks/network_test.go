package checks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type testResolver map[string][]netip.Addr

func (resolver testResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	addresses, ok := resolver[host]
	if !ok {
		return nil, fmt.Errorf("unknown host")
	}
	return addresses, nil
}

type testDialer struct {
	address string
}

func (dialer testDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, dialer.address)
}

func TestNetworkPolicyAllowsTailnetAndConfiguredRFC1918Only(t *testing.T) {
	policy, err := NewNetworkPolicy([]string{"192.168.10.0/24", "10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	policy.Resolver = testResolver{
		"tail.service":    {netip.MustParseAddr("100.64.4.5")},
		"private.service": {netip.MustParseAddr("192.168.10.20")},
		"public.service":  {netip.MustParseAddr("203.0.113.4")},
		"mixed.service":   {netip.MustParseAddr("100.64.4.5"), netip.MustParseAddr("203.0.113.4")},
	}
	for _, test := range []struct {
		host string
		want bool
	}{
		{host: "tail.service", want: true},
		{host: "private.service", want: true},
		{host: "public.service", want: false},
		{host: "mixed.service", want: false},
	} {
		_, resolveErr := policy.Resolve(context.Background(), test.host)
		if (resolveErr == nil) != test.want {
			t.Fatalf("Resolve(%q) error=%v, want allowed=%v", test.host, resolveErr, test.want)
		}
	}
	for _, address := range []string{"127.0.0.1", "169.254.169.254", "224.0.0.1", "0.0.0.0"} {
		if _, resolveErr := policy.Resolve(context.Background(), address); !errors.Is(resolveErr, errInvalidTarget) {
			t.Fatalf("Resolve(%q) error=%v, want invalid target", address, resolveErr)
		}
	}
}

func TestNetworkPolicyRejectsUnsafeConfiguredCIDRs(t *testing.T) {
	for _, cidr := range []string{"0.0.0.0/0", "172.16.0.0/11", "100.64.0.0/10", "::1/128"} {
		if _, err := NewNetworkPolicy([]string{cidr}); !errors.Is(err, ErrInvalidSettings) {
			t.Fatalf("NewNetworkPolicy(%q) error=%v, want invalid settings", cidr, err)
		}
	}
}

func TestHTTPProbeValidatesRedirectsAndUsesValidatedDialer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirect" {
			http.Redirect(response, request, "http://public.service/blocked", http.StatusFound)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})}
	go server.Serve(listener)
	defer server.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	policy, err := NewNetworkPolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	policy.Resolver = testResolver{
		"tail.service":   {netip.MustParseAddr("100.64.4.5")},
		"public.service": {netip.MustParseAddr("203.0.113.4")},
	}
	policy.Dialer = testDialer{address: listener.Addr().String()}
	probe := NewProbe(policy)
	probe.Clock = func() time.Time { return time.Now().UTC() }

	base := fmt.Sprintf("http://tail.service:%d/health", port)
	up := probe.Probe(context.Background(), Config{Type: TypeHTTP, HTTP: &HTTPConfig{URL: base, IntervalSeconds: 60, TimeoutSeconds: 5, ExpectedStatusMin: 200, ExpectedStatusMax: 299, MaxRedirects: 3}})
	if up.Status != StateUp || up.ErrorCode != nil {
		t.Fatalf("successful probe=%+v", up)
	}
	redirect := probe.Probe(context.Background(), Config{Type: TypeHTTP, HTTP: &HTTPConfig{URL: strings.Replace(base, "/health", "/redirect", 1), IntervalSeconds: 60, TimeoutSeconds: 5, ExpectedStatusMin: 200, ExpectedStatusMax: 299, FollowRedirects: true, MaxRedirects: 3}})
	if redirect.Status != StateDown || redirect.ErrorCode == nil || *redirect.ErrorCode != ErrorInvalidTarget {
		t.Fatalf("blocked redirect=%+v", redirect)
	}
}
