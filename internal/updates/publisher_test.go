package updates

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These deterministic secrets are fixture-only. No production policy embeds
// these public keys, and no key generator exists in the packaging entrypoint.
func fixtureLinux(t *testing.T, build uint64) (string, Identity, LinuxVerifier, ed25519.PrivateKey) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	public := key.Public().(ed25519.PublicKey)
	id := sampleIdentity(build)
	id.TeamID = ""
	id.Platform = "linux"
	id.Publisher = Publisher{Kind: LinuxEd25519, ID: digest(public)}
	id.Architecture = "amd64"
	id.MinimumOS = "24.04"
	path := t.TempDir()
	write(t, filepath.Join(path, "bin/Trove"), []byte("fixture executable bytes"))
	if err := os.Chmod(filepath.Join(path, "bin/Trove"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(path, "Resources/web/index.html"), []byte("<p>fixture web resource</p>"))
	verifier := LinuxVerifier{Policy: PublisherPolicy{FormatVersion: 1, LinuxPublicKeys: []string{hex.EncodeToString(public)}}}
	signFixture(t, path, id, key)
	return path, id, verifier, key
}
func signFixture(t *testing.T, path string, id Identity, key ed25519.PrivateKey) {
	t.Helper()
	raw, err := BuildReleaseManifest(context.Background(), path, id)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(path, ReleaseManifestName), raw)
	write(t, filepath.Join(path, LinuxSignatureName), ed25519.Sign(key, LinuxSigningMessage(raw)))
}
func TestPublisherCanonicalLegacyMacAndTaggedCopies(t *testing.T) {
	legacy := sampleIdentity(1)
	tagged := legacy
	tagged.Platform = "darwin"
	tagged.Publisher = Publisher{Kind: AppleDeveloperID, ID: legacy.TeamID}
	tagged.TeamID = ""
	if !SamePublisher(legacy, tagged) || !EqualIdentity(legacy, tagged) {
		t.Fatal("legacy Mac publisher was lost")
	}
	raw, err := json.Marshal(tagged)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Identity
	if err = json.Unmarshal(raw, &decoded); err != nil || !EqualIdentity(tagged, decoded) {
		t.Fatalf("decoded identity changed authority: %v", err)
	}
	tagged.Build++
	if !SamePublisher(legacy, tagged) || EqualIdentity(legacy, tagged) {
		t.Fatal("publisher and full release identity conflated")
	}
}
func TestPublisherRejectsCrossPlatformTypeKeyAndAmbiguity(t *testing.T) {
	_, base, _, _ := fixtureLinux(t, 1)
	for name, change := range map[string]func(*Identity){
		"platform":       func(i *Identity) { i.Platform = "windows"; i.Publisher.Kind = WindowsAuthenticode },
		"kind":           func(i *Identity) { i.Publisher.Kind = AppleDeveloperID },
		"key":            func(i *Identity) { i.Publisher.ID = strings.Repeat("a", 64) },
		"legacy mixture": func(i *Identity) { i.TeamID = "ABCDEFGHIJ" },
		"unlabelled":     func(i *Identity) { i.Platform = "" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			change(&changed)
			if SamePublisher(base, changed) || EqualIdentity(base, changed) {
				t.Fatal("publisher swap accepted")
			}
			changed.Build++
			if VerifyUpgrade(Application{Identity: base, Inventory: Inventory{SHA256: digest([]byte("current"))}}, Application{Identity: changed, Inventory: Inventory{SHA256: digest([]byte("next"))}}) == nil {
				t.Fatal("publisher swap authorized an upgrade")
			}
		})
	}
	bad := sampleIdentity(1)
	bad.Platform = "darwin"
	bad.Publisher = Publisher{Kind: AppleDeveloperID, ID: "ZZZZZZZZZZ"}
	if validateIdentity(bad) == nil {
		t.Fatal("contradictory TeamID accepted")
	}
	for _, raw := range []string{`{"teamId":"ABCDEFGHIJ","teamId":"ZZZZZZZZZZ"}`, `{"teamId":"ABCDEFGHIJ","TEAMID":"ZZZZZZZZZZ"}`, `{"publisher":{"kind":"x","kind":"y"}}`, `{"publisher":{"kind":"x","extra":true}}`} {
		var id Identity
		if json.Unmarshal([]byte(raw), &id) == nil {
			t.Fatalf("ambiguous identity decoded: %s", raw)
		}
	}
}
func TestLinuxSignatureAuthenticatesAllPayloadAndAuthBytes(t *testing.T) {
	for _, mutation := range []string{"none", "web", "binary", "extra", "missing", "mode", "signature", "manifest", "symlink", "foreign-auth"} {
		t.Run(mutation, func(t *testing.T) {
			path, _, v, _ := fixtureLinux(t, 1)
			switch mutation {
			case "web":
				write(t, filepath.Join(path, "Resources/web/index.html"), []byte("tampered"))
			case "binary":
				write(t, filepath.Join(path, "bin/Trove"), []byte("tampered"))
			case "extra":
				write(t, filepath.Join(path, "Resources/plugin"), []byte("unexpected"))
			case "missing":
				os.Remove(filepath.Join(path, "Resources/web/index.html"))
			case "mode":
				os.Chmod(filepath.Join(path, "bin/Trove"), 0400)
			case "signature":
				write(t, filepath.Join(path, LinuxSignatureName), bytes.Repeat([]byte{1}, 64))
			case "manifest":
				raw, _ := os.ReadFile(filepath.Join(path, ReleaseManifestName))
				write(t, filepath.Join(path, ReleaseManifestName), append(raw, '\n'))
			case "symlink":
				os.Symlink(filepath.Join(path, "bin/Trove"), filepath.Join(path, "link"))
			case "foreign-auth":
				write(t, filepath.Join(path, WindowsCatalogName), []byte("foreign"))
			}
			app, err := v.Inspect(context.Background(), path)
			if mutation == "none" {
				if err != nil || len(app.Inventory.Objects) < 5 {
					t.Fatalf("valid exact artifact rejected: %v", err)
				}
			} else if err == nil {
				t.Fatal("unsigned mutation accepted")
			}
		})
	}
}
func TestLinuxUnconfiguredWrongKeyAndDomainSeparatedSignaturesFail(t *testing.T) {
	path, id, v, key := fixtureLinux(t, 1)
	if _, err := (LinuxVerifier{}).Inspect(context.Background(), "/path/not/opened"); err == nil {
		t.Fatal("empty policy allowed")
	}
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x17}, 32)).Public().(ed25519.PublicKey)
	wrong := v
	wrong.Policy.LinuxPublicKeys = []string{hex.EncodeToString(other)}
	if _, err := wrong.Inspect(context.Background(), path); err == nil {
		t.Fatal("foreign pinned key allowed")
	}
	raw, _ := os.ReadFile(filepath.Join(path, ReleaseManifestName))
	write(t, filepath.Join(path, LinuxSignatureName), ed25519.Sign(key, raw))
	if _, err := v.Inspect(context.Background(), path); err == nil {
		t.Fatal("cross-protocol signature allowed")
	}
	id.Platform = "windows"
	id.Publisher.Kind = WindowsAuthenticode
	raw, _ = json.Marshal(ReleaseManifest{FormatVersion: 1, Identity: id})
	write(t, filepath.Join(path, ReleaseManifestName), raw)
	write(t, filepath.Join(path, LinuxSignatureName), ed25519.Sign(key, LinuxSigningMessage(raw)))
	if _, err := v.Inspect(context.Background(), path); err == nil {
		t.Fatal("cross-platform signed manifest accepted")
	}
}
func TestPortableKitVerifiesBothSignaturesBeforeAndAfterStaging(t *testing.T) {
	previous, _, v, _ := fixtureLinux(t, 1)
	next, _, _, _ := fixtureLinux(t, 2)
	current, err := v.Inspect(context.Background(), previous)
	if err != nil {
		t.Fatal(err)
	}
	update, err := v.Inspect(context.Background(), next)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "recovery")
	kit, err := PrepareKit(context.Background(), v, previous, next, destination, current, update, fixtureBackup, fixtureVerify)
	if err != nil || kit.FormatVersion != 2 {
		t.Fatalf("portable kit failed: %v", err)
	}
	if _, err = os.Stat(filepath.Join(destination, "previous/Trove", LinuxSignatureName)); err != nil {
		t.Fatal(err)
	}
	if _, err = InspectKit(context.Background(), v, destination, fixtureVerify); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(destination, "previous/Trove/Resources/web/index.html"), []byte("tampered after staging"))
	if _, err = InspectKit(context.Background(), v, destination, fixtureVerify); err == nil {
		t.Fatal("changed staged resource accepted")
	}
}
func TestLegacyMacKitAcceptsCanonicalTaggedVerifier(t *testing.T) {
	parent := t.TempDir()
	previous, current := fixtureApp(t, parent, "previous", 1)
	next, update := fixtureApp(t, parent, "next", 2)
	destination := filepath.Join(parent, "recovery")
	_, err := PrepareKit(context.Background(), fixtureVerifier{}, previous, next, destination, current, update, fixtureBackup, fixtureVerify)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = InspectKit(context.Background(), taggedMacFixtureVerifier{}, destination, fixtureVerify); err != nil {
		t.Fatalf("old TeamID kit no longer accepted: %v", err)
	}
}

type taggedMacFixtureVerifier struct{}

func (taggedMacFixtureVerifier) Inspect(ctx context.Context, path string) (Application, error) {
	app, err := (fixtureVerifier{}).Inspect(ctx, path)
	app.Identity.Platform = "darwin"
	app.Identity.Publisher = Publisher{Kind: AppleDeveloperID, ID: app.Identity.TeamID}
	return app, err
}
func TestPublisherPolicyAndFullInventoryComparisonFailClosed(t *testing.T) {
	for _, raw := range []string{`{}`, `{"formatVersion":1,"linuxPublicKeys":["placeholder"]}`, `{"formatVersion":1,"windowsCertificates":["A"]}`, `{"formatVersion":1,"formatVersion":1}`, `{"formatVersion":1,"acceptUnsigned":true}`} {
		if _, err := ParsePublisherPolicy([]byte(raw)); err == nil {
			t.Fatalf("invalid trust policy accepted: %s", raw)
		}
	}
	a := Application{Identity: sampleIdentity(1), Inventory: Inventory{SHA256: digest([]byte("same summary")), Objects: []Object{{Path: "good"}}}}
	b := a
	b.Inventory.Objects = []Object{{Path: "other"}}
	if sameApplication(a, b) {
		t.Fatal("different full inventories accepted based only on digest fields")
	}
}
func TestPortableVerificationHonorsCancellationAndPlatformRefusal(t *testing.T) {
	path, _, v, _ := fixtureLinux(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := v.Inspect(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled verifier: %v", err)
	}
	if runtime.GOOS != "windows" {
		if _, err := (WindowsVerifier{Policy: PublisherPolicy{FormatVersion: 1, WindowsCertificates: []string{strings.Repeat("a", 64)}}}).Inspect(context.Background(), path); err == nil {
			t.Fatal("non-native Windows signature substitute admitted")
		}
	}
}
func TestWindowsNativePublisherQualificationPending(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PENDING: real Windows signed catalog/member, cached revocation, changed signer/content, native file streams and staging evidence require Windows")
	}
}

func TestEmptyPortablePinConfigurationAuthorizesNeitherPlatform(t *testing.T) {
	policy, err := ParsePublisherPolicy([]byte(`{"formatVersion":1,"linuxPublicKeys":[],"windowsCertificates":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	id := sampleIdentity(1)
	id.TeamID = ""
	id.Publisher.ID = strings.Repeat("a", 64)
	for _, platform := range []string{"linux", "windows"} {
		id.Platform = platform
		id.Publisher.Kind = LinuxEd25519
		if platform == "windows" {
			id.Publisher.Kind = WindowsAuthenticode
		}
		if policy.AuthorizeIdentity(id) == nil {
			t.Fatal("empty pins authorized portable publisher")
		}
	}
}
