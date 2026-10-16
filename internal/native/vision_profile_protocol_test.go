package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

// This executable fixture exercises real framing, descriptor snapshots, schema
// validation, verified helper hashing and supervisor admission, with tiny dummy
// GGUF bytes. It never loads a model or claims model/native image acceptance.
func init() {
	exe, err := os.Executable()
	if err != nil || filepath.Base(exe) != helperFilename("vision-worker") {
		return
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(exe), "vision-profile-protocol-fixture")); err != nil {
		return
	}
	if err = runVisionProfileProtocolFixture(exe); err != nil {
		os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}
func runVisionProfileProtocolFixture(exe string) error {
	modeBytes, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "vision-profile-protocol-fixture"))
	if err != nil {
		return err
	}
	mode := string(modeBytes)
	hello := map[string]any{"kind": "hello", "protocolVersion": 1, "runtime": "llama.cpp", "runtimeCommit": LlamaCommit, "architecture": runtime.GOARCH, "capabilities": []string{"image_generate", "image_preprocessing_profile_v1"}, "preprocessingProfileId": boundedVisionProfile.ID, "preprocessingProfileHash": boundedVisionProfile.Hash}
	switch mode {
	case "old-helper", "unrelated-old-helper":
		delete(hello, "preprocessingProfileId")
		delete(hello, "preprocessingProfileHash")
		hello["capabilities"] = []string{"image_generate"}
	case "hello-partial":
		delete(hello, "preprocessingProfileHash")
	case "hello-wrong-hash":
		hello["preprocessingProfileHash"] = strings.Repeat("0", 64)
	case "hello-wrong-id":
		hello["preprocessingProfileId"] = "unreviewed-profile-v1"
	case "hello-no-capability":
		hello["capabilities"] = []string{"image_generate"}
	}
	if err = WriteFrame(os.Stdout, hello); err != nil {
		return err
	}
	raw, err := ReadFrame(os.Stdin)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(exe), "request-received"), []byte("received"), 0600); err != nil {
		return err
	}
	if err = contracts.Validate("jobs", "WorkerRequest", raw); err != nil {
		return err
	}
	var wire contracts.JobsWorkerRequest
	if err = json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	input := os.NewFile(6, "metadata")
	var metadata VisionMetadata
	if err = json.NewDecoder(input).Decode(&metadata); err != nil {
		return err
	}
	if strings.HasPrefix(mode, "unrelated-") {
		if metadata.SchemaVersion != 1 || metadata.PreprocessingProfileID != "" || metadata.PreprocessingProfileHash != "" {
			return os.ErrInvalid
		}
	} else if metadata.SchemaVersion != 2 || metadata.PreprocessingProfileID != boundedVisionProfile.ID || metadata.PreprocessingProfileHash != boundedVisionProfile.Hash {
		return os.ErrInvalid
	}
	result := map[string]any{"kind": "image_generation", "text": "Fixture description.", "finishReason": "eos", "inputTokens": 1100, "outputTokens": 4, "preprocessingProfileId": boundedVisionProfile.ID, "preprocessingProfileHash": boundedVisionProfile.Hash}
	switch mode {
	case "result-absent", "unrelated-old-helper", "unrelated-current-helper":
		delete(result, "preprocessingProfileId")
		delete(result, "preprocessingProfileHash")
	case "result-partial":
		delete(result, "preprocessingProfileHash")
	case "result-wrong-id":
		result["preprocessingProfileId"] = "unreviewed-profile-v1"
	case "result-wrong-hash":
		result["preprocessingProfileHash"] = strings.Repeat("0", 64)
	}
	raw, err = json.Marshal(result)
	if err != nil {
		return err
	}
	f := os.NewFile(4, "result")
	if _, err = f.Write(raw); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	return WriteFrame(os.Stdout, map[string]any{"kind": "result", "requestId": wire.RequestId, "leaseToken": wire.LeaseToken, "sequence": 0, "resultHandle": map[string]any{"handleId": wire.RequestId, "kind": "result", "sha256": hex.EncodeToString(hash[:]), "sizeBytes": len(raw)}})
}
func TestVisionProfileSupervisorProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("descriptor-based fixture uses Unix inherited descriptors")
	}
	original := boundedVisionProfile
	model, projector := []byte("GGUFprofile-fixture-model"), []byte("GGUFprofile-fixture-projector")
	p := visionFixture(model, projector)
	// Process-local test substitution of the two fixture hashes only. The profile
	// ID/hash, framing and production request/result validation remain unchanged.
	boundedVisionProfile.ModelSHA256 = p.SHA256
	boundedVisionProfile.ProjectorSHA256 = p.Projector.SHA256
	t.Cleanup(func() { boundedVisionProfile = original })
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(executable)
	for _, mode := range []string{"ok", "unrelated-old-helper", "unrelated-current-helper", "old-helper", "hello-partial", "hello-wrong-hash", "hello-wrong-id", "hello-no-capability", "result-absent", "result-partial", "result-wrong-hash", "result-wrong-id"} {
		t.Run(mode, func(t *testing.T) {
			bundle := t.TempDir()
			helper := filepath.Join(bundle, helperFilename("vision-worker"))
			if err := os.WriteFile(helper, executable, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bundle, "vision-profile-protocol-fixture"), []byte(mode), 0600); err != nil {
				t.Fatal(err)
			}
			m, err := NewManager(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			selected, selectedModel := p, model
			if strings.HasPrefix(mode, "unrelated-") {
				selectedModel = []byte("GGUFunrelated-fixture-model")
				selected = visionFixture(selectedModel, projector)
			}
			if err = m.ImportVision(context.Background(), selected, bytes.NewReader(selectedModel), bytes.NewReader(projector), nil); err != nil {
				t.Fatal(err)
			}
			s := &Supervisor{bundle: bundle, manager: m, helpers: map[string]Helper{"vision-worker": {Name: "vision-worker", SHA256: hex.EncodeToString(digest[:]), Runtime: "llama.cpp", RuntimeCommit: LlamaCommit, Architecture: runtime.GOARCH}}}
			f, err := os.CreateTemp(t.TempDir(), "source-")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			im := image.NewRGBA(image.Rect(0, 0, 8, 8))
			im.SetRGBA(3, 3, color.RGBA{255, 0, 0, 255})
			if err = png.Encode(f, im); err != nil {
				t.Fatal(err)
			}
			result, err := s.GenerateImage(context.Background(), selected.ID, "Describe the source", f, nil)
			if mode == "ok" {
				if err != nil || result.Text != "Fixture description." || result.PreprocessingProfileID != original.ID || result.PreprocessingProfileHash != original.Hash || result.ModelHash != p.SHA256 || result.ProjectorHash != p.Projector.SHA256 || result.HelperHash != hex.EncodeToString(digest[:]) {
					t.Fatal(result, err)
				}
			} else if strings.HasPrefix(mode, "unrelated-") {
				if err != nil || result.Text != "Fixture description." || result.PreprocessingProfileID != "" || result.PreprocessingProfileHash != "" || result.ModelHash != selected.SHA256 || result.ProjectorHash != selected.Projector.SHA256 {
					t.Fatal("unrelated legacy path changed", result, err)
				}
			} else if err == nil || result.Text != "" {
				t.Fatal("invalid profile result admitted", mode, result, err)
			}
			if strings.HasPrefix(mode, "hello-") || mode == "old-helper" {
				if _, err := os.Stat(filepath.Join(bundle, "request-received")); !os.IsNotExist(err) {
					t.Fatal("incompatible helper received request", err)
				}
			} else if _, err := os.Stat(filepath.Join(bundle, "request-received")); err != nil {
				t.Fatal("fixture did not receive bounded request", err)
			}
		})
	}
}
