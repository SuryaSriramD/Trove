package native

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"strings"
	"trove.local/phase0/contracts"
	"unicode/utf8"
)

// VisionMetadata v1 is confined to the pinned SmolVLM chat template and one
// normalized image. Workspace paths and source text never cross this boundary.
type VisionMetadata struct {
	SchemaVersion int    `json:"schemaVersion"`
	Question      string `json:"question"`
	ContextTokens int    `json:"contextTokens"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
}
type VisionResult struct {
	PeakRSSBytes   int64  `json:"sampledPeakRSSBytes,omitempty"`
	AnalysisMethod string `json:"analysisMethod"`
	ModelHash      string `json:"modelHash,omitempty"`
	ProjectorHash  string `json:"projectorHash,omitempty"`
	HelperHash     string `json:"helperHash,omitempty"`
	Kind           string `json:"kind"`
	Text           string `json:"text"`
	FinishReason   string `json:"finishReason"`
	InputTokens    int    `json:"inputTokens"`
	OutputTokens   int    `json:"outputTokens"`
}

func validateVisionInput(p Pack, r Request) (VisionMetadata, error) {
	var v VisionMetadata
	if p.Kind != "vision" || p.VisionVersion != 1 || len(r.Inputs) != 2 || r.Inputs[0].Handle.Kind != "text" || r.Inputs[0].Handle.SizeBytes > 16384 || r.Inputs[1].Handle.Kind != "image" || r.Inputs[1].Handle.SizeBytes > 5<<20 {
		return v, errors.New("INVALID_INPUT: image descriptor layout")
	}
	d := json.NewDecoder(io.NewSectionReader(r.Inputs[0].File, 0, r.Inputs[0].Handle.SizeBytes))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return v, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return v, errors.New("INVALID_INPUT: trailing image metadata")
	}
	if v.SchemaVersion != 1 || v.ContextTokens < 512 || v.ContextTokens > p.ContextTokens || v.ContextTokens > 8192 || v.Width < 1 || v.Height < 1 || v.Width > 1024 || v.Height > 1024 || !utf8.ValidString(v.Question) || utf8.RuneCountInString(v.Question) > 2048 || strings.TrimSpace(v.Question) == "" || r.Wire.Limits.MaxTokens < 1 || r.Wire.Limits.MaxTokens > 512 || r.Wire.Limits.TimeoutMs > 300000 || r.Wire.Limits.Threads > 4 || r.Wire.Limits.MaxOutputBytes > 128<<10 {
		return v, errors.New("INVALID_INPUT: image context or resource bounds")
	}
	cfg, format, err := image.DecodeConfig(io.NewSectionReader(r.Inputs[1].File, 0, r.Inputs[1].Handle.SizeBytes))
	if err != nil || format != "png" || cfg.Width != v.Width || cfg.Height != v.Height {
		return v, errors.New("INVALID_INPUT: normalized image mismatch")
	}
	return v, nil
}

// NormalizeVisionImage preserves original bytes and returns a new private PNG.
// Alpha composites over white. Version 1 uses encoded pixel orientation and
// bounds decode to 20 MP; it never invokes codecs or downloads from the image.
func NormalizeVisionImage(ctx context.Context, f *os.File) (*os.File, int, int, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, 0, 0, err
	}
	if !st.Mode().IsRegular() || st.Size() > 20<<20 {
		return nil, 0, 0, errors.New("INVALID_INPUT: PNG/JPEG image must be at most 20 MiB")
	}
	cfg, format, err := image.DecodeConfig(io.NewSectionReader(f, 0, st.Size()))
	if err != nil || (format != "png" && format != "jpeg") {
		return nil, 0, 0, errors.New("INVALID_INPUT: corrupt or unsupported PNG/JPEG image")
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 20000000 {
		return nil, 0, 0, errors.New("RESOURCE_EXHAUSTED: image exceeds 20 megapixels")
	}
	if err = ctx.Err(); err != nil {
		return nil, 0, 0, err
	}
	src, _, err := image.Decode(io.NewSectionReader(f, 0, st.Size()))
	if err != nil {
		return nil, 0, 0, errors.New("INVALID_INPUT: corrupt image pixels")
	}
	w, h := cfg.Width, cfg.Height
	if max(w, h) > 1024 {
		if w >= h {
			h = max(1, h*1024/w)
			w = 1024
		} else {
			w = max(1, w*1024/h)
			h = 1024
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	// Exact bounded nearest-neighbour sampling keeps normalization reproducible.
	for y := 0; y < h; y++ {
		if err = ctx.Err(); err != nil {
			return nil, 0, 0, err
		}
		for x := 0; x < w; x++ {
			c := color.NRGBAModel.Convert(src.At(src.Bounds().Min.X+x*cfg.Width/w, src.Bounds().Min.Y+y*cfg.Height/h)).(color.NRGBA)
			a := uint32(c.A)
			dst.SetRGBA(x, y, color.RGBA{uint8((uint32(c.R)*a + 255*(255-a)) / 255), uint8((uint32(c.G)*a + 255*(255-a)) / 255), uint8((uint32(c.B)*a + 255*(255-a)) / 255), 255})
		}
	}
	out, err := os.CreateTemp("", "trove-normalized-image-")
	if err != nil {
		return nil, 0, 0, err
	}
	if err = png.Encode(out, dst); err != nil {
		out.Close()
		os.Remove(out.Name())
		return nil, 0, 0, err
	}
	return out, w, h, nil
}
func (s *Supervisor) GenerateImage(ctx context.Context, packID, question string, source *os.File, onToken func(string)) (VisionResult, error) {
	return s.generateImage(ctx, packID, question, source, onToken, true)
}
func (s *Supervisor) generateImage(ctx context.Context, packID, question string, source *os.File, onToken func(string), inspect bool) (VisionResult, error) {
	var result VisionResult
	if !utf8.ValidString(question) || utf8.RuneCountInString(question) > 2048 || strings.TrimSpace(question) == "" {
		return result, errors.New("INVALID_INPUT: image question must contain 1–2048 characters")
	}
	if !s.VisionAvailable() {
		return result, errors.New("WORKER_FAILED: compatible local image helper is unavailable")
	}
	p, _, release, err := s.manager.lease(packID)
	if err != nil {
		return result, err
	}
	defer release()
	if p.Kind != "vision" || p.VisionVersion != 1 || p.QualificationOnly {
		return result, errors.New("MODEL_INCOMPATIBLE: select the verified version 1 vision pair")
	}
	normalized, w, h, err := NormalizeVisionImage(ctx, source)
	if err != nil {
		return result, err
	}
	defer os.Remove(normalized.Name())
	defer normalized.Close()
	if inspect {
		uniform, err := nearlyUniformImage(ctx, normalized)
		if err != nil {
			return result, err
		}
		if uniform {
			return VisionResult{Kind: "image_generation", Text: "Local pixel measurement: this image is nearly uniform. There is too little distinguishable visual variation for a useful description. No model interpretation was attempted.", AnalysisMethod: "pixel_measurement", FinishReason: "measurement"}, nil
		}
	}
	metadata, err := os.CreateTemp("", "trove-image-metadata-")
	if err != nil {
		return result, err
	}
	defer os.Remove(metadata.Name())
	defer metadata.Close()
	err = json.NewEncoder(metadata).Encode(VisionMetadata{SchemaVersion: 1, Question: question, ContextTokens: min(p.ContextTokens, 8192), Width: w, Height: h})
	if err != nil {
		return result, err
	}
	mi, err := OpenInput(metadata, "text", NewID())
	if err != nil {
		return result, err
	}
	ii, err := OpenInput(normalized, "image", NewID())
	if err != nil {
		return result, err
	}
	wire := contracts.JobsWorkerRequest{ProtocolVersion: 1, RequestId: contracts.DomainId(NewID()), JobId: contracts.DomainId(NewID()), LeaseToken: contracts.DomainId(NewID()), CancellationId: contracts.DomainId(NewID()), Operation: "image_generate", ModelPackId: contracts.DomainId(packID), Limits: contracts.JobsResourceLimits{Threads: 4, MemoryBytes: s.MemoryBudget(), TimeoutMs: 300000, MaxOutputBytes: 128 << 10, MaxTokens: 512}}
	metrics := &RunMetrics{}
	raw, err := s.Run(ctx, Request{Wire: wire, Inputs: []Input{mi, ii}, Priority: PriorityChat, Metrics: metrics}, func(e Event) {
		if e.Kind == "token" && onToken != nil {
			onToken(e.Text)
		}
	})
	if err != nil {
		return result, err
	}
	if err = json.NewDecoder(bytes.NewReader(raw)).Decode(&result); err != nil {
		return result, err
	}
	if strings.TrimSpace(result.Text) == "" {
		return result, errors.New("WORKER_FAILED: image model returned no description")
	}
	result.PeakRSSBytes = metrics.PeakRSSBytes.Load()
	result.AnalysisMethod = "local_vision_model"
	if inspect && (result.FinishReason != "eos" || repeatedImageText(result.Text)) {
		return result, errors.New("OUTPUT_LIMIT: image description was incomplete or repetitive; ask a shorter, specific question")
	}
	result.ModelHash = p.SHA256
	result.ProjectorHash = p.Projector.SHA256
	result.HelperHash = s.helpers["vision-worker"].SHA256
	return result, nil
}

func nearlyUniformImage(ctx context.Context, f *os.File) (bool, error) {
	im, _, err := image.Decode(io.NewSectionReader(f, 0, 5<<20))
	if err != nil {
		return false, err
	}
	lo := [3]uint32{65535, 65535, 65535}
	hi := [3]uint32{}
	for y := im.Bounds().Min.Y; y < im.Bounds().Max.Y; y++ {
		if err = ctx.Err(); err != nil {
			return false, err
		}
		for x := im.Bounds().Min.X; x < im.Bounds().Max.X; x++ {
			r, g, b, _ := im.At(x, y).RGBA()
			for i, v := range []uint32{r, g, b} {
				lo[i] = min(lo[i], v)
				hi[i] = max(hi[i], v)
			}
		}
	}
	return hi[0]-lo[0] <= 3*257 && hi[1]-lo[1] <= 3*257 && hi[2]-lo[2] <= 3*257, nil
}
func repeatedImageText(text string) bool {
	words := strings.Fields(strings.ToLower(text))
	seen := map[string]int{}
	for i := 0; i+12 <= len(words); i++ {
		part := strings.Join(words[i:i+12], " ")
		seen[part]++
		if seen[part] >= 3 {
			return true
		}
	}
	return false
}
