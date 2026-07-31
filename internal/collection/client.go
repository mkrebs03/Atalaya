package collection

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/mkrebs03/atalaya/internal/agent"
)

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}
type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type Client struct {
	resolver Resolver
	dialer   Dialer
	roots    *tls.Config
}

func NewClient(resolver Resolver, dialer Dialer, tlsConfig *tls.Config) (*Client, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	if tlsConfig == nil {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if tlsConfig.InsecureSkipVerify {
		return nil, fmt.Errorf("TLS verification cannot be disabled")
	}
	return &Client{resolver: resolver, dialer: dialer, roots: tlsConfig.Clone()}, nil
}

func ValidateAgentURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, &PullError{Kind: FailureProtocol}
	}
	host := strings.ToLower(u.Hostname())
	if net.ParseIP(host) != nil || !strings.HasSuffix(host, ".ts.net") || len(host) <= len(".ts.net") || u.Port() != "9474" || u.Host != net.JoinHostPort(host, "9474") {
		return nil, &PullError{Kind: FailureProtocol}
	}
	return u, nil
}

func (c *Client) Pull(ctx context.Context, claim Claim) (agent.Snapshot, error) {
	u, err := ValidateAgentURL(claim.AgentURL)
	if err != nil {
		return agent.Snapshot{}, err
	}
	transport := &http.Transport{
		Proxy:             nil,
		TLSClientConfig:   c.roots.Clone(),
		DialContext:       c.secureDial(u.Hostname()),
		ForceAttemptHTTP2: true,
	}
	transport.TLSClientConfig.ServerName = u.Hostname()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: claim.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claim.AgentURL+"/agent/v1/snapshot", nil)
	if err != nil {
		return agent.Snapshot{}, &PullError{Kind: FailureInternal}
	}
	req.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(claim.Credential))
	response, err := client.Do(req)
	req.Header.Del("Authorization")
	if err != nil {
		return agent.Snapshot{}, classifyNetworkError(err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return agent.Snapshot{}, &PullError{Kind: FailureAuth}
	}
	if response.StatusCode >= http.StatusInternalServerError {
		return agent.Snapshot{}, &PullError{Kind: FailureConnect}
	}
	if response.StatusCode != http.StatusOK {
		return agent.Snapshot{}, &PullError{Kind: FailureProtocol}
	}
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, MaxPayloadBytes+1))
	if readErr != nil {
		return agent.Snapshot{}, &PullError{Kind: FailurePayload}
	}
	if len(payload) > MaxPayloadBytes {
		return agent.Snapshot{}, &PullError{Kind: FailurePayload}
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var snapshot agent.Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return agent.Snapshot{}, &PullError{Kind: FailureProtocol}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return agent.Snapshot{}, &PullError{Kind: FailureProtocol}
	}
	if err := validateSnapshot(snapshot); err != nil {
		return agent.Snapshot{}, &PullError{Kind: FailureProtocol}
	}
	return snapshot, nil
}

func (c *Client) secureDial(host string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		addresses, err := c.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, &PullError{Kind: FailureDNS}
		}
		for _, address := range addresses {
			if !tailscaleAddress(address.Unmap()) {
				return nil, &PullError{Kind: FailureDNS}
			}
		}
		var last error
		for _, address := range addresses {
			connection, dialErr := c.dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), "9474"))
			if dialErr == nil {
				return connection, nil
			}
			last = dialErr
		}
		return nil, fmt.Errorf("%w: %v", &PullError{Kind: FailureConnect}, last)
	}
}

var tailscaleV4 = netip.MustParsePrefix("100.64.0.0/10")
var tailscaleV6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")

func tailscaleAddress(address netip.Addr) bool {
	return address.IsValid() && (tailscaleV4.Contains(address) || tailscaleV6.Contains(address))
}

func classifyNetworkError(err error) error {
	var pull *PullError
	if errors.As(err, &pull) {
		return pull
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return &PullError{Kind: FailureTimeout}
	}
	var certificate *tls.CertificateVerificationError
	if errors.As(err, &certificate) || strings.Contains(strings.ToLower(err.Error()), "tls") || strings.Contains(strings.ToLower(err.Error()), "certificate") {
		return &PullError{Kind: FailureTLS}
	}
	return &PullError{Kind: FailureConnect}
}

func utcTimestamp(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.IsZero() {
		return time.Time{}, false
	}
	_, offset := parsed.Zone()
	return parsed, offset == 0
}
