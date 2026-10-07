package updates

import "errors"

const (
	AppleDeveloperID    = "apple-developer-id"
	LinuxEd25519        = "linux-ed25519-sha256"
	WindowsAuthenticode = "windows-authenticode-sha256"
)

// Publisher is a value, not a pointer: decoded copies compare by authority, not
// allocation identity. Legacy TeamID-only records canonicalize to darwin below.
type Publisher struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func canonicalPublisher(id Identity) (string, Publisher, error) {
	if id.Platform == "" && id.Publisher == (Publisher{}) {
		if !teamPattern.MatchString(id.TeamID) {
			return "", Publisher{}, errors.New("legacy publisher requires a valid macOS TeamID")
		}
		return "darwin", Publisher{Kind: AppleDeveloperID, ID: id.TeamID}, nil
	}
	p := id.Publisher
	switch id.Platform {
	case "darwin":
		if p.Kind != AppleDeveloperID || !teamPattern.MatchString(p.ID) || id.TeamID != "" && id.TeamID != p.ID {
			return "", Publisher{}, errors.New("ambiguous macOS publisher identity")
		}
	case "linux":
		if p.Kind != LinuxEd25519 || !validDigest(p.ID) || id.TeamID != "" {
			return "", Publisher{}, errors.New("invalid Linux publisher identity")
		}
	case "windows":
		if p.Kind != WindowsAuthenticode || !validDigest(p.ID) || id.TeamID != "" {
			return "", Publisher{}, errors.New("invalid Windows publisher identity")
		}
	default:
		return "", Publisher{}, errors.New("publisher platform must be explicit and supported")
	}
	return id.Platform, p, nil
}
func SamePublisher(a, b Identity) bool {
	platformA, publisherA, errA := canonicalPublisher(a)
	platformB, publisherB, errB := canonicalPublisher(b)
	return errA == nil && errB == nil && a.BundleID == BundleID && b.BundleID == BundleID && platformA == platformB && publisherA == publisherB
}
func EqualIdentity(a, b Identity) bool {
	if validateIdentity(a) != nil || validateIdentity(b) != nil {
		return false
	}
	a.Platform, a.Publisher, _ = canonicalPublisher(a)
	b.Platform, b.Publisher, _ = canonicalPublisher(b)
	a.TeamID = ""
	b.TeamID = ""
	return a == b
}
