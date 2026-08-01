package checks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	errDNS           = errors.New("DNS resolution failed")
	errInvalidTarget = errors.New("target is not allowed")
	errRedirectLimit = errors.New("redirect limit reached")
)

var tailscalePrefix = netip.MustParsePrefix("100.64.0.0/10")
var metadataAddress = netip.MustParseAddr("169.254.169.254")

type LookupResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type ContextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type NetworkPolicy struct {
	PrivateCIDRs []netip.Prefix
	Resolver     LookupResolver
	Dialer       ContextDialer
}

func NewNetworkPolicy(cidrs []string) (NetworkPolicy, error) {
	policy := NetworkPolicy{Resolver: net.DefaultResolver, Dialer: &net.Dialer{KeepAlive: 30 * time.Second}}
	seen := make(map[netip.Prefix]bool, len(cidrs))
	for _, raw := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return NetworkPolicy{}, fmt.Errorf("%w: private CIDR", ErrInvalidSettings)
		}
		prefix = prefix.Masked()
		if !prefix.Addr().Is4() || !isRFC1918(prefix) {
			return NetworkPolicy{}, fmt.Errorf("%w: private CIDR", ErrInvalidSettings)
		}
		if !seen[prefix] {
			seen[prefix] = true
			policy.PrivateCIDRs = append(policy.PrivateCIDRs, prefix)
		}
	}
	if len(policy.PrivateCIDRs) > 32 {
		return NetworkPolicy{}, fmt.Errorf("%w: too many private CIDRs", ErrInvalidSettings)
	}
	return policy, nil
}

func isRFC1918(prefix netip.Prefix) bool {
	if !prefix.Addr().Is4() {
		return false
	}
	for _, private := range []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
	} {
		if private.Contains(prefix.Addr()) && private.Contains(prefixLast(prefix)) {
			return true
		}
	}
	return false
}

func prefixLast(prefix netip.Prefix) netip.Addr {
	address := prefix.Masked().Addr().As4()
	bits := prefix.Bits()
	mask := uint32(0xffffffff) >> bits
	value := uint32(address[0])<<24 | uint32(address[1])<<16 | uint32(address[2])<<8 | uint32(address[3])
	value |= mask
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}

func (policy NetworkPolicy) withDefaults() NetworkPolicy {
	if policy.Resolver == nil {
		policy.Resolver = net.DefaultResolver
	}
	if policy.Dialer == nil {
		policy.Dialer = &net.Dialer{KeepAlive: 30 * time.Second}
	}
	return policy
}

func (policy NetworkPolicy) Resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	policy = policy.withDefaults()
	if address, err := netip.ParseAddr(host); err == nil {
		if address.Zone() != "" || !policy.allowed(address) {
			return nil, errInvalidTarget
		}
		return []netip.Addr{address}, nil
	}
	addresses, err := policy.Resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errDNS, err)
	}
	if len(addresses) == 0 {
		return nil, errDNS
	}
	for _, address := range addresses {
		if !policy.allowed(address) {
			return nil, errInvalidTarget
		}
	}
	return addresses, nil
}

func (policy NetworkPolicy) allowed(address netip.Addr) bool {
	if !address.IsValid() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() || address == metadataAddress {
		return false
	}
	if tailscalePrefix.Contains(address) {
		return true
	}
	for _, prefix := range policy.PrivateCIDRs {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

type Probe struct {
	Policy         NetworkPolicy
	PolicyProvider func(context.Context) (NetworkPolicy, error)
	Clock          func() time.Time
}

func NewProbe(policy NetworkPolicy) Probe {
	return Probe{Policy: policy, Clock: time.Now}
}

func (probe Probe) now() time.Time {
	if probe.Clock == nil {
		return time.Now().UTC()
	}
	return probe.Clock().UTC()
}

func (probe Probe) Probe(ctx context.Context, config Config) Result {
	result := Result{Status: StateDown, ReceivedAt: probe.now()}
	if err := ValidateConfig(config); err != nil {
		code := ErrorInvalidTarget
		result.ErrorCode = &code
		return result
	}
	if probe.PolicyProvider != nil {
		policy, err := probe.PolicyProvider(ctx)
		if err != nil {
			code := ErrorInternal
			result.ErrorCode = &code
			return result
		}
		probe.Policy = policy
	}
	if config.HTTP != nil {
		return probe.http(ctx, *config.HTTP)
	}
	return probe.tcp(ctx, *config.TCP)
}

func (probe Probe) http(parent context.Context, config HTTPConfig) Result {
	start := probe.now()
	result := Result{Status: StateDown, ReceivedAt: start}
	requestContext, cancel := context.WithTimeout(parent, time.Duration(config.TimeoutSeconds)*time.Second)
	defer cancel()
	parsed, _ := url.Parse(config.URL)
	if err := probe.validateURL(requestContext, parsed); err != nil {
		result.ErrorCode = pointer(errorCodeFor(err))
		return probe.completeResult(result, start)
	}
	transport := &http.Transport{
		Proxy:                  nil,
		MaxResponseHeaderBytes: 64 << 10,
		DialContext:            probe.dialContext,
		TLSHandshakeTimeout:    time.Duration(config.TimeoutSeconds) * time.Second,
		ResponseHeaderTimeout:  time.Duration(config.TimeoutSeconds) * time.Second,
	}
	client := &http.Client{Transport: transport}
	if !config.FollowRedirects {
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	} else {
		client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
			if len(via) > config.MaxRedirects {
				return errRedirectLimit
			}
			return probe.validateURL(requestContext, request.URL)
		}
	}
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, config.URL, nil)
	if err != nil {
		code := ErrorInvalidTarget
		result.ErrorCode = &code
		return probe.completeResult(result, start)
	}
	response, err := client.Do(request)
	if err != nil {
		result.ErrorCode = pointer(errorCodeFor(err))
		return probe.completeResult(result, start)
	}
	_ = response.Body.Close()
	if response.StatusCode < config.ExpectedStatusMin || response.StatusCode > config.ExpectedStatusMax {
		code := ErrorHTTPStatus
		result.ErrorCode = &code
		result.HTTPStatus = &response.StatusCode
		return probe.completeResult(result, start)
	}
	result.Status = StateUp
	return probe.completeResult(result, start)
}

func (probe Probe) tcp(parent context.Context, config TCPConfig) Result {
	start := probe.now()
	result := Result{Status: StateDown, ReceivedAt: start}
	requestContext, cancel := context.WithTimeout(parent, time.Duration(config.TimeoutSeconds)*time.Second)
	defer cancel()
	addresses, err := probe.Policy.Resolve(requestContext, config.Host)
	if err != nil {
		result.ErrorCode = pointer(errorCodeFor(err))
		return probe.completeResult(result, start)
	}
	connection, err := probe.dialAddresses(requestContext, "tcp", strconv.Itoa(config.Port), addresses)
	if err != nil {
		result.ErrorCode = pointer(errorCodeFor(err))
		return probe.completeResult(result, start)
	}
	_ = connection.Close()
	result.Status = StateUp
	return probe.completeResult(result, start)
}

func (probe Probe) completeResult(result Result, start time.Time) Result {
	received := probe.now()
	result.ReceivedAt = received
	if result.Status == StateUp {
		latency := elapsedMilliseconds(start, received)
		result.LatencyMS = &latency
	}
	return result
}

func (probe Probe) validateURL(ctx context.Context, target *url.URL) error {
	if target == nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" || target.User != nil || target.Fragment != "" {
		return errInvalidTarget
	}
	if port := target.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return errInvalidTarget
		}
	}
	_, err := probe.Policy.Resolve(ctx, target.Hostname())
	return err
}

func (probe Probe) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := probe.Policy.Resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	return probe.dialAddresses(ctx, network, port, addresses)
}

func (probe Probe) dialAddresses(ctx context.Context, network, port string, addresses []netip.Addr) (net.Conn, error) {
	policy := probe.Policy.withDefaults()
	var last error
	for _, address := range addresses {
		connection, err := policy.Dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
		if err == nil {
			return connection, nil
		}
		last = err
	}
	if last == nil {
		return nil, errDNS
	}
	return nil, last
}

func errorCodeFor(err error) ErrorCode {
	if errors.Is(err, errInvalidTarget) || errors.Is(err, errRedirectLimit) {
		return ErrorInvalidTarget
	}
	if errors.Is(err, errDNS) {
		return ErrorDNS
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrorTimeout
	}
	if strings.Contains(strings.ToLower(err.Error()), "tls") {
		return ErrorTLS
	}
	var operationError *net.OpError
	if errors.As(err, &operationError) {
		return ErrorConnect
	}
	return ErrorConnect
}

func elapsedMilliseconds(start, end time.Time) int64 {
	value := end.Sub(start).Milliseconds()
	if value < 0 {
		return 0
	}
	return value
}

func pointer[T any](value T) *T { return &value }
