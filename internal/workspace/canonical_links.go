package workspace

import (
	"net/url"
	"regexp"
	"strings"
)

var canonicalLinkID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// Canonical page URLs may address a block within the page. Treating the entire
// suffix as a page identity breaks save, recovery and portable ID remapping.
func parseCanonicalPageLink(raw string) (pageID, blockID string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 512 || u.Scheme != "trove" || u.Host != "page" || u.User != nil || u.RawPath != "" {
		return "", "", false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", "", false
	}
	for key, values := range query {
		if key != "workspace" || len(values) != 1 || !canonicalLinkID.MatchString(values[0]) {
			return "", "", false
		}
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 1 && len(parts) != 3 {
		return "", "", false
	}
	pageID = parts[0]
	if !canonicalLinkID.MatchString(pageID) {
		return "", "", false
	}
	if len(parts) == 3 {
		if parts[1] != "block" || u.Fragment != "" {
			return "", "", false
		}
		blockID = parts[2]
	} else if u.Fragment != "" {
		blockID = u.Fragment
	}
	if blockID != "" && !canonicalLinkID.MatchString(blockID) {
		return "", "", false
	}
	if len(parts) == 3 && blockID == "" {
		return "", "", false
	}
	return pageID, blockID, true
}
func canonicalPageLink(pageID, blockID string) string {
	link := "trove://page/" + pageID
	if blockID != "" {
		link += "/block/" + blockID
	}
	return link
}
func portablePageLink(link, source string, pages map[string]string) string {
	pageID, blockID, ok := parseCanonicalPageLink(link)
	if !ok {
		return link
	}
	target := pages[pageID]
	if target == "" {
		return link
	}
	if blockID != "" {
		target += "#" + blockID
	}
	return relativeExportLink(source, target)
}
