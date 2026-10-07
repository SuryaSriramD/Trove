package updates

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

const (
	ReleaseManifestName  = "trove-release.json"
	LinuxSignatureName   = "trove-release.ed25519"
	WindowsCatalogName   = "trove-release.cat"
	releaseManifestLimit = 4 << 20
	linuxSignatureDomain = "Trove portable release v1\x00"
)

// PublisherPolicy is an explicit trusted packaging/public-key configuration.
// Never read it from an untrusted update, renderer payload or recovered kit.
// Empty policy is valid configuration, but authorizes no portable publisher.
type PublisherPolicy struct {
	FormatVersion       int      `json:"formatVersion"`
	LinuxPublicKeys     []string `json:"linuxPublicKeys"`     // Canonical hex Ed25519 public keys.
	WindowsCertificates []string `json:"windowsCertificates"` // SHA256 of leaf certificate DER.
}

func ParsePublisherPolicy(raw []byte) (PublisherPolicy, error) {
	var policy PublisherPolicy
	if len(raw) > 16<<10 {
		return policy, errors.New("publisher policy exceeds bound")
	}
	if err := decodeStrict(raw, &policy); err != nil {
		return policy, err
	}
	if policy.FormatVersion != 1 || len(policy.LinuxPublicKeys) > 16 || len(policy.WindowsCertificates) > 16 {
		return policy, errors.New("unsupported publisher policy")
	}
	seen := map[string]bool{}
	for _, key := range policy.LinuxPublicKeys {
		if !validDigest(key) || seen[key] {
			return policy, errors.New("invalid or duplicate Linux public key")
		}
		seen[key] = true
	}
	clear(seen)
	for _, pin := range policy.WindowsCertificates {
		if !validDigest(pin) || seen[pin] {
			return policy, errors.New("invalid or duplicate Windows publisher pin")
		}
		seen[pin] = true
	}
	return policy, nil
}

type ReleaseManifest struct {
	FormatVersion int       `json:"formatVersion"`
	Identity      Identity  `json:"identity"`
	Inventory     Inventory `json:"inventory"` // Every object except the fixed authentication files.
}
type LinuxVerifier struct{ Policy PublisherPolicy }
type WindowsVerifier struct{ Policy PublisherPolicy }

// BuildReleaseManifest returns canonical bytes for an external signing step.
// It never generates keys, signs, installs, launches or changes an artifact.
func BuildReleaseManifest(ctx context.Context, path string, id Identity) ([]byte, error) {
	if err := validateIdentity(id); err != nil {
		return nil, err
	}
	if id.Platform != "linux" && id.Platform != "windows" {
		return nil, errors.New("portable manifest needs explicit Linux or Windows publisher")
	}
	tree, err := InspectTree(ctx, path)
	if err != nil {
		return nil, err
	}
	payload, err := payloadInventory(tree, id.Platform)
	if err != nil {
		return nil, err
	}
	if err = validatePayload(payload); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(ReleaseManifest{FormatVersion: 1, Identity: id, Inventory: payload})
	if err != nil || len(raw) > releaseManifestLimit {
		return nil, errors.New("release manifest exceeds bound")
	}
	return raw, nil
}
func LinuxSigningMessage(manifest []byte) []byte {
	return append([]byte(linuxSignatureDomain), manifest...)
}
func (v LinuxVerifier) Inspect(ctx context.Context, path string) (Application, error) {
	keys, err := v.Policy.linuxKeys()
	if err != nil {
		return Application{}, err
	}
	return inspectPortable(ctx, path, "linux", func(root *os.Root, manifest []byte, id Identity, authenticated map[string]string) error {
		key, ok := keys[id.Publisher.ID]
		if !ok {
			return errors.New("Linux publisher is not pinned by this installation")
		}
		signature, err := readReleaseFile(root, LinuxSignatureName, ed25519.SignatureSize)
		if err != nil || len(signature) != ed25519.SignatureSize {
			return errors.New("missing or invalid detached Ed25519 release signature")
		}
		sum := sha256.Sum256(signature)
		authenticated[LinuxSignatureName] = hex.EncodeToString(sum[:])
		if !ed25519.Verify(key, LinuxSigningMessage(manifest), signature) {
			return errors.New("release manifest signature is invalid")
		}
		return nil
	})
}
func (p PublisherPolicy) linuxKeys() (map[string]ed25519.PublicKey, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	checked, err := ParsePublisherPolicy(raw)
	if err != nil || len(checked.LinuxPublicKeys) == 0 {
		return nil, errors.New("Linux updates require explicit trusted Ed25519 public-key configuration")
	}
	result := map[string]ed25519.PublicKey{}
	for _, key := range checked.LinuxPublicKeys {
		decoded, _ := hex.DecodeString(key)
		sum := sha256.Sum256(decoded)
		result[hex.EncodeToString(sum[:])] = ed25519.PublicKey(decoded)
	}
	return result, nil
}
func (p PublisherPolicy) windowsPins() (map[string]bool, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	checked, err := ParsePublisherPolicy(raw)
	if err != nil || len(checked.WindowsCertificates) == 0 {
		return nil, errors.New("Windows updates require explicit trusted Authenticode certificate configuration")
	}
	result := map[string]bool{}
	for _, pin := range checked.WindowsCertificates {
		result[pin] = true
	}
	return result, nil
}
func inspectPortable(ctx context.Context, path, platform string, authenticate func(*os.Root, []byte, Identity, map[string]string) error) (Application, error) {
	var out Application
	if err := ctx.Err(); err != nil {
		return out, err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return out, err
	}
	before, err := InspectTree(ctx, path)
	if err != nil {
		return out, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return out, err
	}
	defer root.Close()
	raw, err := readReleaseFile(root, ReleaseManifestName, releaseManifestLimit)
	if err != nil {
		return out, err
	}
	var manifest ReleaseManifest
	if err = decodeStrict(raw, &manifest); err != nil {
		return out, err
	}
	canonical, err := json.Marshal(manifest)
	if err != nil || !bytes.Equal(raw, canonical) || manifest.FormatVersion != 1 {
		return out, errors.New("release manifest must use the canonical supported encoding")
	}
	if err = validateIdentity(manifest.Identity); err != nil {
		return out, err
	}
	if manifest.Identity.Platform != platform {
		return out, errors.New("signed release platform differs from verifier")
	}
	if err = validatePayload(manifest.Inventory); err != nil {
		return out, err
	}
	authenticated := map[string]string{}
	if err = authenticate(root, raw, manifest.Identity, authenticated); err != nil {
		return out, err
	}
	after, err := InspectTree(ctx, path)
	if err != nil {
		return out, err
	}
	if before.SHA256 != after.SHA256 {
		return out, errors.New("release artifact changed during authentication")
	}
	actual, err := payloadInventory(after, platform)
	if err != nil {
		return out, err
	}
	if !reflect.DeepEqual(actual, manifest.Inventory) {
		return out, errors.New("artifact contains missing, extra or changed signed inventory objects")
	}
	// Bind the bytes decoded/authenticated above to the exact returned full tree.
	sum := sha256.Sum256(raw)
	found := false
	for _, object := range after.Objects {
		if object.Path == ReleaseManifestName {
			found = object.SHA256 == hex.EncodeToString(sum[:]) && object.Size == int64(len(raw))
		}
	}
	if !found {
		return out, errors.New("authenticated manifest differs from artifact inventory")
	}
	for name, expected := range authenticated {
		matched := false
		for _, object := range after.Objects {
			if object.Path == name {
				matched = !object.Directory && object.SHA256 == expected
			}
		}
		if !matched {
			return out, errors.New("authentication file changed during verification")
		}
	}
	out.Identity = manifest.Identity
	out.Inventory = after
	return out, nil
}
func readReleaseFile(root *os.Root, name string, limit int64) ([]byte, error) {
	st, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > limit {
		return nil, errors.New("invalid bounded release authentication file")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(st, opened) {
		return nil, errors.New("release authentication file changed")
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != st.Size() || int64(len(raw)) > limit {
		return nil, errors.New("release authentication file exceeded its bound")
	}
	return raw, nil
}
func payloadInventory(tree Inventory, platform string) (Inventory, error) {
	var out Inventory
	signature := LinuxSignatureName
	if platform == "windows" {
		signature = WindowsCatalogName
	}
	for _, object := range tree.Objects {
		if object.Path == ReleaseManifestName || object.Path == signature {
			if object.Directory {
				return out, errors.New("authentication names cannot be directories")
			}
			continue
		}
		if object.Path == LinuxSignatureName || object.Path == WindowsCatalogName {
			return out, errors.New("foreign-platform authentication file is not permitted")
		}
		out.Objects = append(out.Objects, object)
		out.Bytes += object.Size
	}
	raw, _ := json.Marshal(out.Objects)
	sum := sha256.Sum256(raw)
	out.SHA256 = hex.EncodeToString(sum[:])
	return out, nil
}
func validatePayload(inventory Inventory) error {
	if len(inventory.Objects) == 0 || len(inventory.Objects) > maxBundleFiles || inventory.Bytes < 0 || inventory.Bytes > maxBundleBytes || !validDigest(inventory.SHA256) {
		return errors.New("invalid release payload inventory")
	}
	var total int64
	seen := map[string]bool{}
	for _, object := range inventory.Objects {
		name := object.Path
		if !fs.ValidPath(name) || name == "." || len(name) > 4096 || strings.ContainsAny(name, "\\\x00\r\n") || name == "INCOMPLETE" || name == ReleaseManifestName || name == LinuxSignatureName || name == WindowsCatalogName || object.Mode&^0777 != 0 || object.Mode&0022 != 0 {
			return errors.New("invalid or ambiguous release payload path/mode")
		}
		if seen[strings.ToLower(name)] {
			return errors.New("case-ambiguous release payload paths")
		}
		seen[strings.ToLower(name)] = true
		if object.Directory {
			if object.Size != 0 || object.SHA256 != "" {
				return errors.New("directory payload metadata must be empty")
			}
		} else {
			if object.Size < 0 || object.Size > maxBundleBytes-total || !validDigest(object.SHA256) {
				return errors.New("invalid release payload bytes")
			}
			total += object.Size
		}
	}
	raw, _ := json.Marshal(inventory.Objects)
	sum := sha256.Sum256(raw)
	if total != inventory.Bytes || hex.EncodeToString(sum[:]) != inventory.SHA256 {
		return errors.New("release payload summary differs from its complete inventory")
	}
	return nil
}

// Reject duplicate keys, unknown fields, excessive depth and trailing JSON. A
// signature authenticates bytes, but contradictory metadata is still refused.
func decodeStrict(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > releaseManifestLimit {
		return errors.New("invalid bounded JSON metadata")
	}
	scan := json.NewDecoder(bytes.NewReader(raw))
	scan.UseNumber()
	nodes := 0
	var value func(int) error
	value = func(depth int) error {
		nodes++
		if depth > 16 || nodes > 150000 {
			return errors.New("JSON metadata exceeds structural bounds")
		}
		token, err := scan.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for scan.More() {
				key, err := scan.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[strings.ToLower(name)] {
					return errors.New("duplicate JSON metadata field")
				}
				seen[strings.ToLower(name)] = true
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for scan.More() {
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, err = scan.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := scan.Token(); err != io.EOF {
		return errors.New("trailing JSON metadata")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	return nil
}
func (id *Identity) UnmarshalJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("identity must be a JSON object")
	}
	type plain Identity
	var value plain
	if err := decodeStrict(raw, &value); err != nil {
		return err
	}
	*id = Identity(value)
	return nil
}

// AuthorizeIdentity checks explicit packaging policy before preparing signing
// input. Candidate-provided policy is never an acceptable trust source.
func (p PublisherPolicy) AuthorizeIdentity(id Identity) error {
	if err := validateIdentity(id); err != nil {
		return err
	}
	switch id.Platform {
	case "linux":
		keys, err := p.linuxKeys()
		if err != nil {
			return err
		}
		if _, ok := keys[id.Publisher.ID]; ok {
			return nil
		}
	case "windows":
		pins, err := p.windowsPins()
		if err != nil {
			return err
		}
		if pins[id.Publisher.ID] {
			return nil
		}
	}
	return errors.New("release publisher is not authorized by explicit packaging policy")
}
