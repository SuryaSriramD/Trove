package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var errMetadata = errors.New("client metadata could not be verified")

func httpsURL(raw string, requirePath bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\\") || u.RawPath != "" || strings.HasSuffix(u.Hostname(), ".") {
		return nil, errMetadata
	}
	if requirePath && (u.Path == "" || u.Path == "/") {
		return nil, errMetadata
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return nil, errMetadata
		}
	}
	return u, nil
}
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.User != nil || u.Fragment != "" || u.Hostname() == "" || strings.ContainsAny(raw, "\r\n\\") || u.RawPath != "" {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}
func publicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, text := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "64:ff9b::/96", "64:ff9b:1::/48"} {
		if netip.MustParsePrefix(text).Contains(addr) {
			return false
		}
	}
	return true
}

// fetchMetadata pins the approved public DNS result for the actual connection.
// Redirects, proxies, private addresses, alternate ports and client URLs with
// credentials/query/fragment are forbidden. No user-provided logo/URI is fetched.
func fetchMetadata(ctx context.Context, raw string) (clientMetadata, error) {
	u, err := httpsURL(raw, true)
	if err != nil || u.Port() != "" && u.Port() != "443" {
		return clientMetadata{}, errMetadata
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", u.Hostname())
	if err != nil || len(ips) == 0 || len(ips) > 32 {
		return clientMetadata{}, errMetadata
	}
	for _, ip := range ips {
		if !publicAddress(ip) {
			return clientMetadata{}, errMetadata
		}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !strings.EqualFold(host, u.Hostname()) || port != "443" {
			return nil, errMetadata
		}
		return dialer.DialContext(ctx, "tcp", net.JoinHostPort(ips[0].String(), "443"))
	}}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errMetadata }}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return clientMetadata{}, errMetadata
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return clientMetadata{}, errMetadata
	}
	defer resp.Body.Close()
	contentType, _, typeErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != 200 || typeErr != nil || contentType != "application/json" {
		return clientMetadata{}, errMetadata
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(data) > 65536 {
		return clientMetadata{}, errMetadata
	}
	var meta clientMetadata
	if json.Unmarshal(data, &meta) != nil || meta.ID != raw || validateMetadata(meta) != nil {
		return clientMetadata{}, errMetadata
	}
	return meta, nil
}
func validateMetadata(meta clientMetadata) error {
	if meta.Name == "" || len(meta.Name) > 200 || strings.ContainsAny(meta.Name, "\r\n") || len(meta.RedirectURIs) < 1 || len(meta.RedirectURIs) > 10 {
		return errMetadata
	}
	for _, uri := range meta.RedirectURIs {
		if !validRedirect(uri) {
			return errMetadata
		}
	}
	// CIMD's plural method list takes precedence during the current transition.
	if len(meta.AuthMethods) > 0 {
		if !contains(meta.AuthMethods, "none") {
			return errMetadata
		}
	} else if meta.AuthMethod != "" && meta.AuthMethod != "none" {
		return errMetadata
	}
	if len(meta.GrantTypes) > 2 || len(meta.ResponseTypes) > 1 {
		return errMetadata
	}
	for _, grant := range meta.GrantTypes {
		if grant != "authorization_code" && grant != "refresh_token" {
			return errMetadata
		}
	}
	if len(meta.GrantTypes) > 0 && !contains(meta.GrantTypes, "authorization_code") {
		return errMetadata
	}
	if len(meta.ResponseTypes) > 0 && !contains(meta.ResponseTypes, "code") {
		return errMetadata
	}
	return nil
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
