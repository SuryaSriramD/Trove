package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const MaxResponseBytes = 8 << 20

// ValidateEndpoint is shared by registration and requests. DNS answers are
// checked again at connect time; a public name cannot redirect us onto the LAN.
func ValidateEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return fail("INVALID_ENDPOINT", "Connectors require an HTTPS endpoint on port 443 without embedded credentials")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !publicIP(ip) {
		return fail("INVALID_ENDPOINT", "Local and private network endpoints are not supported")
	}
	return nil
}
func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.Equal(net.IPv4(169, 254, 169, 254)) && !cgnat.Contains(ip)
}

var cgnat = func() *net.IPNet { _, n, _ := net.ParseCIDR("100.64.0.0/10"); return n }()

func NewHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	t := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 25 * time.Second, MaxIdleConns: 8, MaxConnsPerHost: 4, IdleConnTimeout: 60 * time.Second}
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fail("INVALID_ENDPOINT", "Invalid connector host")
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, fail("NETWORK_UNAVAILABLE", "Connector host could not be resolved")
		}
		for _, ip := range ips {
			if !publicIP(ip.IP) {
				return nil, fail("INVALID_ENDPOINT", "Connector resolved to a private network address")
			}
		}
		for _, ip := range ips {
			var c net.Conn
			c, err = dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return c, nil
			}
		}
		return nil, fail("NETWORK_UNAVAILABLE", "Connector could not establish a connection")
	}
	return &http.Client{Transport: endpointTransport{t}, Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return fail("REDIRECT_DENIED", "Connector redirects require a new explicit request")
	}}
}

type endpointTransport struct{ next http.RoundTripper }

func (t endpointTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := ValidateEndpoint(r.URL.String()); err != nil {
		return nil, err
	}
	return t.next.RoundTrip(r)
}
func allowedAPI(a Account, target string) bool {
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	switch a.Kind {
	case GoogleCalendar, GoogleDrive, Gmail:
		return u.Host == "www.googleapis.com" || u.Host == "gmail.googleapis.com" || u.Host == "openidconnect.googleapis.com"
	case GitHub:
		return u.Host == "api.github.com"
	case Notion:
		return u.Host == "api.notion.com" || u.Host == "mcp.notion.com"
	case MCP:
		expected, err := url.Parse(a.Endpoint)
		return err == nil && expected.Scheme == u.Scheme && expected.Host == u.Host && expected.Path == u.Path
	}
	return false
}

// request never forwards a bearer token to a URL received in remote content.
// Each adapter constructs URLs on its fixed API origin (MCP uses its separately
// trusted registration endpoint). Mutation transport failures are ambiguous.
func request(ctx context.Context, s Session, method, target string, input any, headers map[string]string, output any) (http.Header, error) {
	if err := ValidateEndpoint(target); err != nil {
		return nil, err
	}
	if !allowedAPI(s.Account, target) {
		return nil, fail("INVALID_ENDPOINT", "The requested API does not belong to this connector")
	}
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil || len(data) > MaxResponseBytes {
			return nil, fail("INVALID_REQUEST", "Connector request is too large or invalid")
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fail("INVALID_REQUEST", "Connector request is invalid")
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.Token.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token.AccessToken)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := s.Client
	if client == nil {
		client = NewHTTPClient()
	}
	mutating := method != http.MethodGet && method != http.MethodHead
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		if mutating {
			return nil, Unknown("The request may have reached the provider; reconcile its outcome before any retry")
		}
		return nil, fail("NETWORK_UNAVAILABLE", "Connector request did not complete")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Providers can commit before an upstream 5xx. Never retry a write on the
		// strength of that status alone. No response body enters diagnostics.
		if mutating && resp.StatusCode >= 500 {
			return resp.Header, Unknown("Provider returned an uncertain server error; reconcile before retrying")
		}
		limited := resp.StatusCode == 429
		if resp.StatusCode == 403 {
			limited = resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Remaining") == "0"
			if !limited && (s.Account.Kind == GoogleCalendar || s.Account.Kind == GoogleDrive || s.Account.Kind == Gmail) {
				var details struct {
					Error struct {
						Errors []struct {
							Reason string `json:"reason"`
						} `json:"errors"`
					} `json:"error"`
				}
				data, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
				if readErr == nil && json.Unmarshal(data, &details) == nil {
					for _, item := range details.Error.Errors {
						if item.Reason == "rateLimitExceeded" || item.Reason == "userRateLimitExceeded" {
							limited = true
						}
					}
				}
			}
		}
		if limited {
			seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			if seconds < 1 {
				seconds = 60
			}
			if seconds > 86400 {
				seconds = 86400
			}
			return resp.Header, &Failure{Code: "RATE_LIMITED", Message: "Provider rate limit reached", RetryAfter: time.Duration(seconds) * time.Second}
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return resp.Header, fail("ACCESS_REVOKED", "Provider denied this account or selected resource")
		}
		if resp.StatusCode == 404 || resp.StatusCode == 410 {
			return resp.Header, fail("REMOTE_MISSING", "Selected resource is missing or inaccessible")
		}
		if resp.StatusCode == 409 || resp.StatusCode == 412 {
			return resp.Header, fail("REMOTE_CHANGED", "Remote content changed; prepare a new review")
		}
		return resp.Header, fail("PROVIDER_REJECTED", "Provider rejected the reviewed request")
	}
	if output == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, MaxResponseBytes+1))
		if err != nil && mutating {
			return resp.Header, Unknown("Response was interrupted; reconcile before retrying")
		}
		return resp.Header, err
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil || len(b) > MaxResponseBytes || json.Unmarshal(b, output) != nil {
		if mutating {
			return resp.Header, Unknown("Provider response could not be verified; reconcile before retrying")
		}
		return resp.Header, fail("INVALID_RESPONSE", "Provider returned an invalid or oversized response")
	}
	return resp.Header, nil
}
func queryURL(base string, values url.Values) string {
	if len(values) == 0 {
		return base
	}
	return base + "?" + values.Encode()
}
func pathPart(s string) string { return url.PathEscape(s) }
func bounded(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
func stringify(v any) string { b, _ := json.Marshal(v); return string(b) }
func nonempty(s string) bool { return strings.TrimSpace(s) != "" }
