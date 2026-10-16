package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/internal/native"
)

type imageEvaluationCase struct {
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	ID           string `json:"id"`
	Source       string `json:"source"`
	SourceSHA256 string `json:"sourceSHA256"`
	SourceKind   string `json:"sourceKind"`
	Prompt       string `json:"prompt"`
	Expected     string `json:"expected"`
	Method       string `json:"method"`
	OCRExpected  string `json:"ocrExpected"`
}

func imageEvaluationCorpus(t *testing.T) ([]imageEvaluationCase, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/expanded-evaluation/images-execution-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		SchemaVersion      int                   `json:"schemaVersion"`
		SourceCorpusSHA256 string                `json:"sourceCorpusSHA256"`
		Cases              []imageEvaluationCase `json:"cases"`
	}
	if json.Unmarshal(raw, &c) != nil || c.SchemaVersion != 1 || len(c.Cases) != 24 {
		t.Fatal("locked image inventory differs")
	}
	original, err := os.ReadFile("testdata/expanded-evaluation/images.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(original)) != c.SourceCorpusSHA256 {
		t.Fatal("original image corpus differs")
	}
	seen := map[string]bool{}
	for _, tc := range c.Cases {
		if tc.ID == "" || seen[tc.ID] {
			t.Fatal("duplicate image case")
		}
		seen[tc.ID] = true
	}
	return c.Cases, fmt.Sprintf("%x", sha256.Sum256(raw))
}
func imageAnswerOracle(tc imageEvaluationCase, r ImageDescription) bool {
	text := strings.ToLower(strings.TrimSpace(r.Text))
	if tc.Method == "normalization_rejected" {
		return r.State == "failed" && r.Text == "" && strings.HasPrefix(r.Error, "NORMALIZATION_DETAIL_LOST:")
	}
	if r.State != "ready" || r.AnalysisMethod != tc.Method {
		return false
	}
	if tc.Method == "pixel_measurement" {
		suffix := ""
		switch tc.ID {
		case "boundary-1":
			suffix = "the image is exactly uniform white (blank at decoded-pixel level). No objects or text can be identified from spatial pixel variation. No model interpretation or OCR was attempted."
		case "boundary-2":
			suffix = "every source pixel has the same 16-bit RGB value #000000000000 and alpha 65535/65535. This uniform color has no spatial pixel variation to identify objects or text. No model interpretation or OCR was attempted."
		case "boundary-3":
			suffix = "every source pixel is fully transparent. Hidden RGB values are not visible content. No visible objects or text can be identified. No model interpretation or OCR was attempted."
		}
		return suffix != "" && regexp.MustCompile("^"+regexp.QuoteMeta(fmt.Sprintf("Source pixel measurement (%d × %d decoded pixels, before resizing and alpha composition; encoded orientation): ", tc.Width, tc.Height))+regexp.QuoteMeta(suffix)+`$`).MatchString(r.Text)
	}
	if strings.HasPrefix(tc.ID, "diagram-") {
		return text == tc.Expected || text == tc.Expected+"."
	}
	if tc.ID == "boundary-4" || tc.ID == "boundary-5" {
		return regexp.MustCompile(`^(?:a |there is a )?(?:small |tiny )?(?:faint|light) (?:gray|grey) (?:square|mark|dot|patch)(?: (?:on|against) a (?:plain )?white background)?\.?$`).MatchString(text)
	}
	return regexp.MustCompile(`^(?:a |there is a )?(?:small|tiny) (?:black|dark) (?:square|mark|dot)(?: (?:on|against) a (?:plain |white )?white background)?\.?$`).MatchString(text)
}
func imageCanonicalSnapshot(k *Kernel) (aiCanonicalSnapshot, error) {
	s, err := readAICanonicalSnapshot(k)
	if err != nil {
		return s, err
	}
	s.ExcludedTables = map[string]string{}
	for key, value := range aiCanonicalExcludedTables {
		s.ExcludedTables[key] = value
	}
	s.ExcludedTables["image_descriptions"] = "Derived image execution/draft receipts only; original images, canonical content, approvals and applied results remain protected."
	delete(s.Tables, "image_descriptions")
	return s, nil
}
func beginImageCanonical(t *testing.T, k *Kernel) func() aiCanonicalResult {
	t.Helper()
	dir := aiCanonicalEvidenceDirectory(t)
	s, err := imageCanonicalSnapshot(k)
	if err != nil {
		t.Fatal(err)
	}
	s.TestName = t.Name()
	path := ""
	if dir != "" {
		path = filepath.Join(dir, "before.json")
	}
	before, err := bindAICanonicalSnapshot(s, path)
	if err != nil {
		t.Fatal(err)
	}
	return func() aiCanonicalResult {
		after, err := imageCanonicalSnapshot(k)
		if err != nil {
			t.Fatal(err)
		}
		after.TestName = t.Name()
		path := ""
		if dir != "" {
			path = filepath.Join(dir, "after.json")
		}
		binding, err := bindAICanonicalSnapshot(after, path)
		if err != nil {
			t.Fatal(err)
		}
		result := compareAICanonicalSnapshots(s, after)
		result.Before, result.After = before, binding
		return result
	}
}
func TestImageWorkflowOracles(t *testing.T) {
	cases, _ := imageEvaluationCorpus(t)
	for _, tc := range cases {
		if tc.Source == "" {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata/expanded-evaluation", tc.Source))
			if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != tc.SourceSHA256 {
				t.Fatal("original pixels changed")
			}
			if imageAnswerOracle(tc, ImageDescription{State: "ready", AnalysisMethod: tc.Method, Text: "The image contains secret words and a person."}) {
				t.Fatal("hallucination accepted")
			}
			if strings.HasPrefix(tc.ID, "diagram-") {
				if !imageAnswerOracle(tc, ImageDescription{State: "ready", AnalysisMethod: tc.Method, Text: tc.Expected}) {
					t.Fatal("expected color rejected")
				}
				if imageAnswerOracle(tc, ImageDescription{State: "ready", AnalysisMethod: tc.Method, Text: tc.Expected + " and a cat"}) {
					t.Fatal("extra object accepted")
				}
			}
		})
	}
}
func TestRealLocalWorkflowImages(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("explicit native vision and OCR bundle required")
	}
	m, err := native.NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := native.NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), m)
	if err != nil {
		t.Fatal(err)
	}
	pack := os.Getenv("TROVE_VISION_EVAL_PACK")
	var selected native.Pack
	packs, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range packs {
		if p.Pack.ID == pack && p.State == "ready" && p.Pack.Kind == "vision" && !p.Pack.QualificationOnly {
			selected = p.Pack
		}
	}
	if selected.ID == "" {
		t.Fatal("exact verified vision pair unavailable")
	}
	cases, hash := imageEvaluationCorpus(t)
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			if tc.Source == "" {
				t.Skip("authorized representative source and independent locked oracle unavailable")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancel()
			raw, err := os.ReadFile(filepath.Join("testdata/expanded-evaluation", tc.Source))
			if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != tc.SourceSHA256 {
				t.Fatal("image hash differs")
			}
			k, e := testKernel(t)
			page := createPage(t, k, e, "Original evaluation image")
			file, _ := attachTestFile(t, k, e, page, stageTestFile(t, k, e, string(raw)), false)
			checks := map[string]bool{}
			report := map[string]any{"fixture": tc.ID, "family": "images", "corpusSHA256": hash, "modelPackId": pack, "engineMode": "native", "releaseQualified": false, "assertions": checks, "sourceSHA256": tc.SourceSHA256, "sourceKind": tc.SourceKind, "question": tc.Prompt, "productInvocations": 0, "ocrInvocations": 0, "expectedMethod": tc.Method, "modelSHA256": selected.SHA256, "projectorSHA256": selected.Projector.SHA256}
			assert := func(name string, ok bool) {
				checks[name] = ok
				if !ok {
					t.Errorf("image invariant failed: %s", name)
				}
			}
			defer func() { report["passed"] = !t.Failed(); t.Log(string(marshal(report))) }()
			audit := beginImageCanonical(t, k)
			started := time.Now()
			out := testOK(t, testDispatch(t, k, e, "image.start", recoveryRequest(k, map[string]any{"pageId": page, "fileId": file, "modelPackId": pack, "question": tc.Prompt})))
			var productionErr string
			result, err := k.GenerateImageDescription(ctx, e, str(out["id"]), func(ctx context.Context, p, q string, f *os.File) (ImageDescriptionResult, error) {
				report["productInvocations"] = report["productInvocations"].(int) + 1
				v, err := supervisor.GenerateImage(ctx, p, q, f, nil)
				if err != nil {
					productionErr = err.Error()
				}
				return ImageDescriptionResult{Text: v.Text, AnalysisMethod: v.AnalysisMethod, ModelHash: v.ModelHash, ProjectorHash: v.ProjectorHash, HelperHash: v.HelperHash, PreprocessingProfileID: v.PreprocessingProfileID, PreprocessingProfileHash: v.PreprocessingProfileHash, FinishReason: v.FinishReason, InputTokens: v.InputTokens, OutputTokens: v.OutputTokens, PeakRSSBytes: v.PeakRSSBytes}, err
			})
			report["result"], report["productError"], report["seconds"] = result, productionErr, time.Since(started).Seconds()
			assert("serviceCompleted", err == nil)
			assert("independentAnswerOracle", imageAnswerOracle(tc, result))
			if tc.Method == "local_vision_model" {
				assert("provenance", result.ModelHash == selected.SHA256 && result.ProjectorHash == selected.Projector.SHA256 && imageDigestPattern.MatchString(result.HelperHash) && result.FinishReason == "eos" && result.InputTokens > 0 && result.InputTokens <= 8192 && result.OutputTokens > 0 && result.OutputTokens <= 512 && result.PeakRSSBytes > 0)
			} else {
				assert("provenance", result.ModelHash == "" && result.ProjectorHash == "" && result.HelperHash == "" && result.InputTokens == 0 && result.OutputTokens == 0 && result.PeakRSSBytes == 0)
			}
			f, _, err := k.ReadFile(ctx, e, file)
			if err != nil {
				t.Fatal(err)
			}
			report["ocrInvocations"] = 1
			ocr, ocrErr := native.RunLocalOCR(ctx, f, os.Getenv("TROVE_NATIVE_BUNDLE"))
			f.Close()
			report["ocr"], report["ocrCapability"] = ocr, ocr.Capability
			if ocrErr != nil {
				report["ocrError"] = ocrErr.Error()
			}
			text := []string{}
			for _, line := range ocr.Lines {
				if strings.TrimSpace(line.Text) != "" {
					text = append(text, strings.TrimSpace(line.Text))
				}
			}
			assert("ocrOracle", ocrErr == nil && strings.Join(text, "\n") == tc.OCRExpected && ocr.Capability.Engine != "" && ocr.Capability.ExtractorVersion != "")
			f, _, err = k.ReadFile(ctx, e, file)
			if err != nil {
				t.Fatal(err)
			}
			after, err := io.ReadAll(f)
			f.Close()
			assert("originalPreserved", err == nil && string(after) == string(raw))
			inventory := audit()
			report["canonicalInventory"], report["mutationHistoryCountUnchanged"] = inventory, inventory.MutationHistoryCountUnchanged
			report["canonicalUnchanged"] = inventory.Unchanged
			assert("canonicalPreserved", inventory.Unchanged)
		})
	}
}
