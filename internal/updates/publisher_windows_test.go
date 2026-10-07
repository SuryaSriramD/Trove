package updates

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeWindowsSignedCatalogPublisher(t *testing.T) {
	artifact, policyPath := os.Getenv("TROVE_WINDOWS_PUBLISHER_ARTIFACT"), os.Getenv("TROVE_WINDOWS_PUBLISHER_POLICY")
	if artifact == "" && policyPath == "" {
		t.Skip("PENDING: explicit real Windows signed-catalog artifact and trusted public publisher policy are required")
	}
	if artifact == "" || policyPath == "" {
		t.Fatal("both native artifact and trusted public policy are required")
	}
	if PathsOverlap(artifact, policyPath) {
		t.Fatal("native trust policy must be selected independently outside the artifact")
	}
	raw, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := ParsePublisherPolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	verifier := WindowsVerifier{Policy: policy}
	app, err := verifier.Inspect(context.Background(), artifact)
	if err != nil {
		t.Fatal(err)
	}
	wrong := policy
	wrong.WindowsCertificates = []string{strings.Repeat("a", 64)}
	if _, err = (WindowsVerifier{Policy: wrong}).Inspect(context.Background(), artifact); err == nil {
		t.Fatal("wrong native signer pin accepted")
	}
	// Only an owned temporary copy is modified. Input release evidence is retained.
	staged := filepath.Join(t.TempDir(), "copy")
	if err = CopyTree(context.Background(), artifact, staged, app.Inventory); err != nil {
		t.Fatal(err)
	}
	if _, err = verifier.Inspect(context.Background(), staged); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(staged, ReleaseManifestName)
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, ' ')
	if err = os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = verifier.Inspect(context.Background(), staged); err == nil {
		t.Fatal("changed catalog member accepted")
	}
}
func TestNativeWindowsNamedStreamsRefuseIncompleteInventory(t *testing.T) {
	path := t.TempDir()
	file := filepath.Join(path, "payload")
	if err := os.WriteFile(file, []byte("visible"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file+":hidden", []byte("not in default stream"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectTree(context.Background(), path); err == nil {
		t.Fatal("unaccounted alternate stream accepted")
	}
}

func TestNativeWindowsDirectoryStreamsRefuseIncompleteInventory(t *testing.T) {
	for _, child := range []bool{false, true} {
		t.Run(map[bool]string{false: "root", true: "child"}[child], func(t *testing.T) {
			root := t.TempDir()
			directory := root
			if child {
				directory = filepath.Join(root, "child")
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, "payload"), []byte("visible"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(directory+":hidden", []byte("hidden directory bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := InspectTree(context.Background(), root); err == nil {
				t.Fatal("unaccounted directory stream accepted")
			}
		})
	}
}
