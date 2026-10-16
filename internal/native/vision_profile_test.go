package native

import (
	"encoding/json"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func exactVisionProfilePack() Pack {
	p := visionFixture([]byte("GGUFmodel"), []byte("GGUFprojector"))
	p.SHA256 = boundedVisionProfile.ModelSHA256
	p.Projector.SHA256 = boundedVisionProfile.ProjectorSHA256
	p.RuntimeCommit = boundedVisionProfile.RuntimeCommit
	return p
}
func TestVisionProfileSelectsOnlyExactTrustedPair(t *testing.T) {
	p := exactVisionProfilePack()
	v := visionProfileMetadata(p, "Describe", 769, 257)
	if v.SchemaVersion != 2 || v.PreprocessingProfileID != "smolvlm-500m-idefics3-bounded-v1" || len(v.PreprocessingProfileHash) != 64 || validateVisionProfile(p, v) != nil {
		t.Fatal(v)
	}
	for name, mutate := range map[string]func(*Pack){
		"model":     func(p *Pack) { p.SHA256 = strings.Repeat("0", 64) },
		"projector": func(p *Pack) { p.Projector.SHA256 = strings.Repeat("0", 64) },
		"runtime":   func(p *Pack) { p.RuntimeCommit = "other" },
		"kind":      func(p *Pack) { p.Kind = "chat" },
		"version":   func(p *Pack) { p.VisionVersion = 2 },
		"absent":    func(p *Pack) { p.Projector = nil },
	} {
		t.Run(name, func(t *testing.T) {
			copy := p
			child := *p.Projector
			copy.Projector = &child
			mutate(&copy)
			if visionProfileForPack(copy) != nil {
				t.Fatal("unrelated pair acquired a profile")
			}
			legacy := visionProfileMetadata(copy, "Describe", 769, 257)
			if legacy.SchemaVersion != 1 || legacy.PreprocessingProfileID != "" || legacy.PreprocessingProfileHash != "" || validateVisionProfile(copy, legacy) != nil {
				t.Fatal(legacy)
			}
			if validateVisionProfile(copy, v) == nil {
				t.Fatal("profile accepted for unrelated pair")
			}
		})
	}
}
func TestVisionProfileRejectsMissingOrChangedMetadata(t *testing.T) {
	p := exactVisionProfilePack()
	expected := visionProfileMetadata(p, "Describe", 512, 512)
	for name, mutate := range map[string]func(*VisionMetadata){
		"legacy":        func(v *VisionMetadata) { *v = VisionMetadata{SchemaVersion: 1} },
		"no-id":         func(v *VisionMetadata) { v.PreprocessingProfileID = "" },
		"other-id":      func(v *VisionMetadata) { v.PreprocessingProfileID = "unreviewed" },
		"no-hash":       func(v *VisionMetadata) { v.PreprocessingProfileHash = "" },
		"other-hash":    func(v *VisionMetadata) { v.PreprocessingProfileHash = strings.Repeat("f", 64) },
		"wrong-version": func(v *VisionMetadata) { v.SchemaVersion = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			v := expected
			mutate(&v)
			if validateVisionProfile(p, v) == nil {
				t.Fatal("missing profile accepted", v)
			}
		})
	}
}
func TestVisionProfileRequiresHelloAndFinalAcknowledgment(t *testing.T) {
	v := visionProfileMetadata(exactVisionProfilePack(), "Describe", 512, 512)
	for _, caps := range [][]string{nil, {"image_generate"}, {"image_generate", "image_preprocessing_profile_v1"}} {
		if validateVisionProfileHello(v, Event{Capabilities: caps}) == nil {
			t.Fatal("old or incompatible helper accepted", caps)
		}
	}
	if err := validateVisionProfileHello(v, Event{Capabilities: []string{"image_generate", "image_preprocessing_profile_v1"}, PreprocessingProfileID: v.PreprocessingProfileID, PreprocessingProfileHash: v.PreprocessingProfileHash}); err != nil {
		t.Fatal(err)
	}
	valid := VisionResult{PreprocessingProfileID: v.PreprocessingProfileID, PreprocessingProfileHash: v.PreprocessingProfileHash}
	if validateVisionProfileResult(v, valid) != nil {
		t.Fatal("valid acknowledgment rejected")
	}
	for _, invalid := range []VisionResult{{}, {PreprocessingProfileID: v.PreprocessingProfileID}, {PreprocessingProfileHash: v.PreprocessingProfileHash}, {PreprocessingProfileID: v.PreprocessingProfileID, PreprocessingProfileHash: strings.Repeat("0", 64)}} {
		if validateVisionProfileResult(v, invalid) == nil {
			t.Fatal("invalid acknowledgment accepted")
		}
	}
	legacy := VisionMetadata{SchemaVersion: 1}
	if validateVisionProfileHello(legacy, Event{Capabilities: []string{"image_generate"}}) != nil || validateVisionProfileResult(legacy, VisionResult{}) != nil {
		t.Fatal("unrelated legacy helper behavior changed")
	}
	if validateVisionProfileResult(legacy, valid) == nil {
		t.Fatal("fabricated legacy provenance accepted")
	}
}
func TestVisionProfileDeclaresRetainedTransportAndReferenceLimits(t *testing.T) {
	var p struct {
		Geometry struct {
			LongestEdge, TileSize int
			Resample              string
			Mean, Std             []float64
		}
		Transport struct {
			Version, MaxEdge, MaxSourcePixels, MaxSourceBytes int
			Sampling, Orientation, Alpha                      string
		}
		Reference struct{ OriginalConversionProcessorProven bool }
	}
	if err := json.Unmarshal(visionProfileJSON, &p); err != nil {
		t.Fatal(err)
	}
	if p.Geometry.LongestEdge != 2048 || p.Geometry.TileSize != 512 || p.Geometry.Resample != "lanczos" || p.Transport.Version != 1 || p.Transport.MaxEdge != 1024 || p.Transport.Sampling != "nearest" || p.Transport.Orientation != "encoded" || p.Transport.Alpha != "white-integer-floor" || p.Transport.MaxSourcePixels != 20000000 || p.Transport.MaxSourceBytes != 20<<20 || p.Reference.OriginalConversionProcessorProven {
		t.Fatal(p)
	}
	if len(p.Geometry.Mean) != 3 || len(p.Geometry.Std) != 3 {
		t.Fatal(p)
	}
	for i := 0; i < 3; i++ {
		if p.Geometry.Mean[i] != .5 || p.Geometry.Std[i] != .5 {
			t.Fatal(p)
		}
	}
}

func TestVisionProfileWireContractPairsBoundedProvenance(t *testing.T) {
	for _, definition := range []string{"WorkerOutput", "WorkerResult"} {
		var value map[string]any
		if definition == "WorkerOutput" {
			value = map[string]any{"kind": "hello", "protocolVersion": 1, "runtime": "llama.cpp", "runtimeCommit": LlamaCommit, "architecture": "arm64", "capabilities": []string{"image_generate", "image_preprocessing_profile_v1"}}
		} else {
			value = map[string]any{"kind": "image_generation", "text": "Fixture", "finishReason": "eos", "inputTokens": 100, "outputTokens": 2}
		}
		validate := func() error {
			b, err := json.Marshal(value)
			if err != nil {
				return err
			}
			return contracts.Validate("jobs", definition, b)
		}
		if err := validate(); err != nil {
			t.Fatal("legacy optional provenance rejected", definition, err)
		}
		value["preprocessingProfileId"] = boundedVisionProfile.ID
		if validate() == nil {
			t.Fatal("unpaired ID accepted", definition)
		}
		value["preprocessingProfileHash"] = boundedVisionProfile.Hash
		if err := validate(); err != nil {
			t.Fatal("current bounded profile rejected", definition, err)
		}
		for _, bad := range []string{"", strings.Repeat("a", 101), "<remote-profile>"} {
			value["preprocessingProfileId"] = bad
			if validate() == nil {
				t.Fatal("invalid ID accepted", definition)
			}
		}
		value["preprocessingProfileId"] = boundedVisionProfile.ID
		value["preprocessingProfileHash"] = "wrong"
		if validate() == nil {
			t.Fatal("invalid digest accepted", definition)
		}
		delete(value, "preprocessingProfileId")
		value["preprocessingProfileHash"] = boundedVisionProfile.Hash
		if validate() == nil {
			t.Fatal("unpaired digest accepted", definition)
		}
	}
}
