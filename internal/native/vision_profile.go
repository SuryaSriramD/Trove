package native

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
)

// Trusted application code: never selected by model metadata, remote text, or
// a workspace option. Identity includes the retained transport limitation.
//
//go:embed vision_profile_v1.json
var visionProfileJSON []byte

type visionPreprocessingProfile struct {
	ID              string `json:"id"`
	ModelSHA256     string `json:"modelSHA256"`
	ProjectorSHA256 string `json:"projectorSHA256"`
	RuntimeCommit   string `json:"runtimeCommit"`
	Hash            string `json:"-"`
}

var boundedVisionProfile = func() visionPreprocessingProfile {
	var p visionPreprocessingProfile
	if err := json.Unmarshal(visionProfileJSON, &p); err != nil {
		panic(err)
	}
	hash := sha256.Sum256(visionProfileJSON)
	p.Hash = hex.EncodeToString(hash[:])
	return p
}()

func visionProfileForPack(p Pack) *visionPreprocessingProfile {
	if p.Kind == "vision" && p.VisionVersion == 1 && p.RuntimeCommit == boundedVisionProfile.RuntimeCommit && p.SHA256 == boundedVisionProfile.ModelSHA256 && p.Projector != nil && p.Projector.SHA256 == boundedVisionProfile.ProjectorSHA256 {
		return &boundedVisionProfile
	}
	return nil
}
func visionProfileMetadata(p Pack, question string, width, height int) VisionMetadata {
	v := VisionMetadata{SchemaVersion: 1, Question: question, ContextTokens: min(p.ContextTokens, 8192), Width: width, Height: height}
	if profile := visionProfileForPack(p); profile != nil {
		v.SchemaVersion = 2
		v.PreprocessingProfileID, v.PreprocessingProfileHash = profile.ID, profile.Hash
	}
	return v
}
func validateVisionProfile(p Pack, v VisionMetadata) error {
	if profile := visionProfileForPack(p); profile != nil {
		if v.SchemaVersion == 2 && v.PreprocessingProfileID == profile.ID && v.PreprocessingProfileHash == profile.Hash {
			return nil
		}
		return errors.New("MODEL_INCOMPATIBLE: required image preprocessing profile is missing or changed")
	}
	if v.SchemaVersion != 1 || v.PreprocessingProfileID != "" || v.PreprocessingProfileHash != "" {
		return errors.New("MODEL_INCOMPATIBLE: image preprocessing profile does not match this verified pair")
	}
	return nil
}
func validateVisionProfileHello(v VisionMetadata, hello Event) error {
	if v.PreprocessingProfileHash != "" && (!slices.Contains(hello.Capabilities, "image_preprocessing_profile_v1") || hello.PreprocessingProfileID != v.PreprocessingProfileID || hello.PreprocessingProfileHash != v.PreprocessingProfileHash) {
		return errors.New("WORKER_FAILED: image helper does not acknowledge the required preprocessing profile; rebuild or update the helper")
	}
	return nil
}
func validateVisionProfileResult(v VisionMetadata, result VisionResult) error {
	if result.PreprocessingProfileID != v.PreprocessingProfileID || result.PreprocessingProfileHash != v.PreprocessingProfileHash {
		return errors.New("WORKER_FAILED: image helper returned missing or incompatible preprocessing provenance")
	}
	return nil
}
