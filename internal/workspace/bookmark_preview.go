package workspace

import (
	"context"
	"errors"
	"golang.org/x/net/html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"trove.local/phase0/contracts"
	"unicode/utf8"
)

const bookmarkMaxBytes = 1 << 20

func init() { RegisterQuery("editor.bookmarkPreview", bookmarkPreview) }
func bookmarkPreview(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := contracts.Validate("service", "BookmarkPreviewRequest", marshal(r)); err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	if e.Actor.Kind != "user" || e.Origin != "user" || str(r["consent"]) != "fetch_once" {
		return nil, Fail("SCOPE_DENIED", "Only an explicit user request may fetch bookmark metadata")
	}
	if err := k.authorizePage(ctx, k.db, e, str(r["pageId"])); err != nil {
		return nil, err
	}
	client := bookmarkHTTPClient(net.DefaultResolver.LookupIPAddr, (&net.Dialer{Timeout: 2 * time.Second}).DialContext)
	defer client.CloseIdleConnections()
	title, description, err := fetchBookmarkMetadata(ctx, client, str(r["url"]))
	if err != nil {
		return nil, Fail("INVALID_REQUEST", "Bookmark metadata is unavailable. The saved content is unchanged.")
	}
	return map[string]any{"sourceUrl": str(r["url"]), "title": title, "description": description, "fetchedAt": now()}, nil
}

var bookmarkDeniedNetworks = func() []netip.Prefix {
	out := []netip.Prefix{}
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "168.63.129.16/32", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func bookmarkPublicIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	if a.Is6() && !netip.MustParsePrefix("2000::/3").Contains(a) {
		return false
	}
	for _, p := range bookmarkDeniedNetworks {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
func bookmarkURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443") || len(raw) > 8192 {
		return nil, errors.New("only public HTTPS destinations are supported")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !bookmarkPublicIP(ip) {
		return nil, errors.New("destination is not public")
	}
	return u, nil
}
func bookmarkHTTPClient(lookup func(context.Context, string) ([]net.IPAddr, error), dial func(context.Context, string, string) (net.Conn, error)) *http.Client {
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 4 * time.Second, MaxResponseHeaderBytes: 64 << 10}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, errors.New("unsupported destination")
		}
		resolved, err := lookup(ctx, host)
		if err != nil || len(resolved) == 0 || len(resolved) > 16 {
			return nil, errors.New("destination resolution failed")
		}
		for _, ip := range resolved {
			if ip.Zone != "" || !bookmarkPublicIP(ip.IP) {
				return nil, errors.New("destination is not public")
			}
		}
		var failure error
		for _, ip := range resolved {
			conn, err := dial(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			failure = err
		}
		return nil, failure
	}
	return &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("redirect budget exhausted")
		}
		if _, err := bookmarkURL(req.URL.String()); err != nil {
			return err
		}
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		req.Header.Del("Referer")
		return nil
	}}
}
func bookmarkText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
func fetchBookmarkMetadata(ctx context.Context, client *http.Client, raw string) (string, string, error) {
	u, err := bookmarkURL(raw)
	if err != nil {
		return "", "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Accept", "text/html, application/xhtml+xml")
	request.Header.Set("User-Agent", "TroveBookmark/1.0")
	response, err := client.Do(request)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", errors.New("source returned an error")
	}
	kind, params, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || (kind != "text/html" && kind != "application/xhtml+xml") {
		return "", "", errors.New("source is not HTML")
	}
	if charset := strings.ToLower(params["charset"]); charset != "" && charset != "utf-8" && charset != "utf8" && charset != "us-ascii" {
		return "", "", errors.New("source charset is unsupported")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, bookmarkMaxBytes+1))
	if err != nil || len(body) > bookmarkMaxBytes || !utf8.Valid(body) {
		return "", "", errors.New("source exceeds metadata limits")
	}
	tokenizer := html.NewTokenizer(strings.NewReader(string(body)))
	title, description, ogTitle := "", "", ""
	insideTitle := false
	for {
		token := tokenizer.Next()
		if token == html.ErrorToken {
			if tokenizer.Err() != io.EOF {
				return "", "", tokenizer.Err()
			}
			break
		}
		tag := tokenizer.Token()
		switch token {
		case html.StartTagToken, html.SelfClosingTagToken:
			if tag.Data == "title" {
				insideTitle = true
			}
			if tag.Data == "meta" {
				attrs := map[string]string{}
				for _, attr := range tag.Attr {
					attrs[strings.ToLower(attr.Key)] = attr.Val
				}
				name := strings.ToLower(attrs["property"])
				if name == "" {
					name = strings.ToLower(attrs["name"])
				}
				switch name {
				case "og:title":
					if ogTitle == "" {
						ogTitle = attrs["content"]
					}
				case "description", "og:description":
					if description == "" {
						description = attrs["content"]
					}
				}
			}
		case html.EndTagToken:
			if tag.Data == "title" {
				insideTitle = false
			}
		case html.TextToken:
			if insideTitle {
				title = bookmarkText(title+tag.Data, 512)
			}
		}
	}
	if ogTitle != "" {
		title = ogTitle
	}
	title = bookmarkText(title, 512)
	if title == "" {
		title = u.Hostname()
	}
	return title, bookmarkText(description, 2048), nil
}
