package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"trove.local/phase0/internal/updates"
)

func TestManifestCommandRequiresExternalConfiguredPolicyAndNeverSigns(t *testing.T) {
	root := t.TempDir()
	artifact := filepath.Join(root, "artifact")
	if err := os.Mkdir(artifact, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifact, "payload"), []byte("fixture bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	// Public fixture material only; this command never receives a private key.
	public := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, 32)).Public().(ed25519.PublicKey)
	sum := sha256.Sum256(public)
	id := updates.Identity{Platform: "linux", Publisher: updates.Publisher{Kind: updates.LinuxEd25519, ID: hex.EncodeToString(sum[:])}, BundleID: updates.BundleID, Version: "1.0.0", Build: 1, UserVersion: 3, StorageGeneration: 1, Architecture: "amd64", MinimumOS: "24.04"}
	identity := filepath.Join(root, "identity.json")
	body, _ := json.Marshal(id)
	os.WriteFile(identity, body, 0600)
	policyPath := filepath.Join(root, "policy.json")
	os.WriteFile(policyPath, []byte(`{"formatVersion":1,"linuxPublicKeys":[],"windowsCertificates":[]}`), 0600)
	output := filepath.Join(artifact, updates.ReleaseManifestName)
	args := []string{"--artifact", artifact, "--identity", identity, "--publisher-policy", policyPath, "--output", output}
	if err := run(args); err == nil {
		t.Fatal("unconfigured publisher produced signing input")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("failed trust policy wrote output")
	}
	policy := updates.PublisherPolicy{FormatVersion: 1, LinuxPublicKeys: []string{hex.EncodeToString(public)}}
	body, _ = json.Marshal(policy)
	os.WriteFile(policyPath, body, 0600)
	candidatePolicy := filepath.Join(artifact, "self-authorized.json")
	if err := os.WriteFile(candidatePolicy, body, 0600); err != nil {
		t.Fatal(err)
	}
	candidateArgs := append([]string{}, args...)
	candidateArgs[5] = candidatePolicy
	if err := run(candidateArgs); err == nil {
		t.Fatal("candidate-provided trust policy accepted")
	}
	if err := os.Remove(candidatePolicy); err != nil {
		t.Fatal(err)
	}
	if err := run(args); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := updates.BuildReleaseManifest(context.Background(), artifact, id)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("noncanonical signing input: %v", err)
	}
	if _, err := os.Stat(filepath.Join(artifact, updates.LinuxSignatureName)); !os.IsNotExist(err) {
		t.Fatal("command unexpectedly signed artifact")
	}
	if err := run(args); err == nil {
		t.Fatal("existing evidence overwritten")
	}
	after, _ := os.ReadFile(output)
	if !bytes.Equal(after, actual) {
		t.Fatal("refused rerun changed evidence")
	}
}
