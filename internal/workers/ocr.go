package workers

import (
	"context"
	"errors"
	"os"
	"reflect"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

// ConfigureOCR binds the verified engine identity, language and model digest to
// status/results. Missing or invalid optional bundles remain unavailable.
func ConfigureOCR(k *workspace.Kernel, bundle string) {
	capability, err := native.OCRCapabilities(bundle)
	if err != nil {
		k.SetOCRProvider(nil)
		return
	}
	k.SetOCRProviderWithCapability(func(ctx context.Context, f *os.File) (workspace.OCRResult, error) {
		result, err := native.RunLocalOCR(ctx, f, bundle)
		if err == nil && !reflect.DeepEqual(result.Capability, capability) {
			return workspace.OCRResult{}, errors.New("OCR engine changed; reopen the workspace before retrying")
		}
		out := workspace.OCRResult{Warning: result.Warning}
		for _, line := range result.Lines {
			out.Lines = append(out.Lines, workspace.OCRLine{Text: line.Text, Confidence: line.Confidence})
		}
		return out, err
	}, workspace.OCRCapability{Engine: capability.Engine, ExtractorVersion: capability.ExtractorVersion, Languages: capability.Languages, Limitations: capability.Limitations, ModelSHA256: capability.ModelSHA256})
}
