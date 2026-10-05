package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/contracts"
)

func visionFixture(model, projector []byte) Pack {
	p := fixturePack(model)
	p.SchemaVersion = 2
	p.Kind = "vision"
	p.VisionVersion = 1
	p.ContextTokens = 8192
	sum := sha256.Sum256(projector)
	p.Projector = &VisionArtifact{ID: NewID(), SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(projector)), License: "MIT", LicenseURL: p.LicenseURL, SourceRevision: "fixture"}
	return p
}
func TestVisionPairRequiresBothHashesAndLeases(t *testing.T) {
	model, projector := []byte("GGUFmodel"), []byte("GGUFprojector")
	p := visionFixture(model, projector)
	m, _ := NewManager(t.TempDir())
	if err := m.Import(context.Background(), p, bytes.NewReader(model), nil); err == nil {
		t.Fatal("single artifact accepted")
	}
	if err := m.ImportVision(context.Background(), p, bytes.NewReader(model), bytes.NewReader([]byte("GGUFincorrect")), nil); err == nil {
		t.Fatal("wrong companion accepted")
	}
	if err := m.ImportVision(context.Background(), p, bytes.NewReader(model), bytes.NewReader(projector), nil); err != nil {
		t.Fatal(err)
	}
	states, err := m.List()
	if err != nil || len(states) != 1 || states[0].State != "ready" || states[0].DownloadedBytes != p.DownloadBytes() {
		t.Fatalf("pair status: %+v %v", states, err)
	}
	_, release, err := m.leaseProjector(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Remove(p.ID) == nil {
		t.Fatal("removed active companion")
	}
	release()
	path, _, _ := m.paths(p.projectorPack())
	if err = os.WriteFile(path, []byte("GGUFbadbytes!"), 0600); err != nil {
		t.Fatal(err)
	}
	states, err = m.List()
	if err != nil || states[0].State != "corrupt" {
		t.Fatal(states, err)
	}
	if _, _, err = m.leaseProjector(p); err == nil {
		t.Fatal("tampered companion leased")
	}
	if err = m.Remove(p.ID); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(m.root)
	if len(entries) != 0 {
		t.Fatal("orphaned pair", entries)
	}
}
func TestVisionManifestRejectsMixedVersions(t *testing.T) {
	p := visionFixture([]byte("GGUFmodel"), []byte("GGUFprojector"))
	for _, mutate := range []func(*Pack){func(p *Pack) { p.SchemaVersion = 1 }, func(p *Pack) { p.VisionVersion = 2 }, func(p *Pack) { p.Projector.ID = p.ID }, func(p *Pack) { p.Projector.SHA256 = "wrong" }, func(p *Pack) { p.ContextTokens = 9999 }, func(p *Pack) { p.Kind = "chat" }} {
		copy := p
		child := *p.Projector
		copy.Projector = &child
		mutate(&copy)
		if copy.Validate() == nil {
			t.Fatalf("accepted %+v", copy)
		}
	}
}
func TestVisionNormalizationPreservesOriginalAndBounds(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "image")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	src := image.NewNRGBA(image.Rect(0, 0, 2048, 512))
	src.SetNRGBA(10, 10, color.NRGBA{R: 255, A: 128})
	if err = png.Encode(f, src); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(f.Name())
	n, w, h, err := NormalizeVisionImage(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	defer os.Remove(n.Name())
	if w != 1024 || h != 256 {
		t.Fatal(w, h)
	}
	got, _, err := image.Decode(io.NewSectionReader(n, 0, 5<<20))
	if err != nil {
		t.Fatal(err)
	}
	if got.At(0, 0) != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatal("alpha was not composited")
	}
	after, _ := os.ReadFile(f.Name())
	if !bytes.Equal(original, after) {
		t.Fatal("original changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err = NormalizeVisionImage(ctx, f); err != context.Canceled {
		t.Fatal(err)
	}
	bad, _ := os.CreateTemp(t.TempDir(), "bad")
	defer bad.Close()
	bad.WriteString("not an image")
	if _, _, _, err = NormalizeVisionImage(context.Background(), bad); err == nil {
		t.Fatal("corrupt image accepted")
	}
}
func TestVisionApprovedAcquisition(t *testing.T) {
	if os.Getenv("TROVE_ACQUIRE_VISION") != "1" {
		t.Skip("explicit approved acquisition only")
	}
	root := os.Getenv("TROVE_MODEL_ROOT")
	if root == "" {
		t.Fatal("model root required")
	}
	m, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	packs, err := LoadCatalog(filepath.Join("..", "..", "native"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range packs {
		if p.Kind == "vision" {
			if err = m.Acquire(context.Background(), p, nil); err != nil {
				t.Fatal(err)
			}
			if err = m.verifyProjector(p); err != nil {
				t.Fatal(err)
			}
			t.Logf("verified %s and %s: %d bytes", p.SHA256, p.Projector.SHA256, p.DownloadBytes())
			return
		}
	}
	t.Fatal("no approved vision pair")
}
func TestVisionRealRubric(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_VISION_INTEGRATION") != "1" {
		t.Skip("explicit real vision qualification")
	}
	m, err := NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), m)
	if err != nil {
		t.Fatal(err)
	}
	scenes := []struct {
		name, question string
		draw           func(*image.RGBA)
		accept         func(string) bool
	}{
		{"red_square", "What color is the square? Answer briefly.", func(im *image.RGBA) {
			draw.Draw(im, image.Rect(64, 64, 192, 192), image.NewUniform(color.RGBA{255, 0, 0, 255}), image.Point{}, draw.Src)
		}, func(s string) bool { return strings.Contains(s, "red") }},
		{"spatial", "Which side is the blue circle on relative to the red square?", func(im *image.RGBA) {
			draw.Draw(im, image.Rect(25, 85, 100, 160), image.NewUniform(color.RGBA{255, 0, 0, 255}), image.Point{}, draw.Src)
			for y := 75; y < 170; y++ {
				for x := 155; x < 245; x++ {
					if (x-200)*(x-200)+(y-122)*(y-122) < 42*42 {
						im.SetRGBA(x, y, color.RGBA{0, 0, 255, 255})
					}
				}
			}
		}, func(s string) bool { return strings.Contains(s, "right") }},
		{"labeled_chart", "In the chart, what are the values for A and B?", visionChartFixture, func(s string) bool { return strings.Contains(s, "2") && strings.Contains(s, "4") }},
		{"blank_uncertainty", "What objects and text can you identify?", func(im *image.RGBA) {}, func(s string) bool {
			return (strings.Contains(s, "blank") || strings.Contains(s, "no object") || strings.Contains(s, "no text")) && !strings.Contains(s, "black text") && !strings.Contains(s, "text reads")
		}},
	}
	results := []map[string]any{}
	for _, test := range scenes {
		im := image.NewRGBA(image.Rect(0, 0, 256, 256))
		draw.Draw(im, im.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		test.draw(im)
		f, _ := os.CreateTemp(t.TempDir(), test.name)
		if err = png.Encode(f, im); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		result, runErr := s.generateImage(context.Background(), "01990000-0000-7000-8000-000000000107", test.question, f, nil, false)
		f.Close()
		passed := runErr == nil && result.FinishReason == "eos" && test.accept(strings.ToLower(result.Text))
		fixtureBytes, readErr := os.ReadFile(f.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		fixtureHash := sha256.Sum256(fixtureBytes)
		row := map[string]any{"case": test.name, "question": test.question, "passed": passed, "result": result, "seconds": time.Since(start).Seconds(), "fixtureSha256": hex.EncodeToString(fixtureHash[:]), "fixtureWidth": 256, "fixtureHeight": 256}
		if runErr != nil {
			row["error"] = runErr.Error()
		}
		results = append(results, row)
		t.Logf("%s passed=%v: %s error=%v", test.name, passed, result.Text, runErr)
	}
	raw, _ := json.MarshalIndent(results, "", "  ")
	if path := os.Getenv("TROVE_VISION_REPORT"); path != "" {
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range results {
		if !row["passed"].(bool) {
			t.Errorf("rubric failed: %s", row["case"])
		}
	}
}

func TestVisionUniformMeasurementAndRepetitionGuard(t *testing.T) {
	model, projector := []byte("GGUFmodel"), []byte("GGUFprojector")
	p := visionFixture(model, projector)
	m, _ := NewManager(t.TempDir())
	if err := m.ImportVision(context.Background(), p, bytes.NewReader(model), bytes.NewReader(projector), nil); err != nil {
		t.Fatal(err)
	}
	s := &Supervisor{manager: m, helpers: map[string]Helper{"vision-worker": {Name: "vision-worker"}}}
	f, _ := os.CreateTemp(t.TempDir(), "uniform")
	defer f.Close()
	png.Encode(f, image.NewRGBA(image.Rect(0, 0, 40, 30)))
	r, err := s.GenerateImage(context.Background(), p.ID, "Describe", f, nil)
	if err != nil || r.AnalysisMethod != "pixel_measurement" || r.InputTokens != 0 || r.ModelHash != "" {
		t.Fatal(r, err)
	}
	if !repeatedImageText(strings.Repeat("one two three four five six seven eight nine ten eleven twelve ", 4)) || repeatedImageText("One red square.") {
		t.Fatal("repetition boundary")
	}
}

func visionChartFixture(im *image.RGBA) {
	draw.Draw(im, image.Rect(50, 125, 95, 200), image.NewUniform(color.RGBA{R: 230, A: 255}), image.Point{}, draw.Src)
	draw.Draw(im, image.Rect(150, 50, 195, 200), image.NewUniform(color.RGBA{B: 230, A: 255}), image.Point{}, draw.Src)
	glyphs := map[rune][]string{'A': {"01110", "10001", "10001", "11111", "10001", "10001", "10001"}, 'B': {"11110", "10001", "10001", "11110", "10001", "10001", "11110"}, '2': {"01110", "10001", "00001", "00010", "00100", "01000", "11111"}, '4': {"00010", "00110", "01010", "10010", "11111", "00010", "00010"}}
	label := func(ch rune, x, y int) {
		for j, row := range glyphs[ch] {
			for i, c := range row {
				if c == '1' {
					draw.Draw(im, image.Rect(x+i*3, y+j*3, x+(i+1)*3, y+(j+1)*3), image.NewUniform(color.Black), image.Point{}, draw.Src)
				}
			}
		}
	}
	label('A', 65, 210)
	label('B', 165, 210)
	label('2', 65, 98)
	label('4', 165, 23)
}

func TestVisionRealCancellationAndExactBudget(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_VISION_INTEGRATION") != "1" {
		t.Skip("explicit native image boundary qualification")
	}
	m, err := NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), m)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.CreateTemp(t.TempDir(), "chart")
	defer f.Close()
	im := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.Draw(im, im.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	visionChartFixture(im)
	png.Encode(f, im)
	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()
	sawToken := false
	_, err = s.GenerateImage(ctx, "01990000-0000-7000-8000-000000000107", "Describe the chart in detail", f, func(string) { sawToken = true; cancel() })
	cancel()
	if !sawToken || !errors.Is(err, context.Canceled) {
		t.Fatal("cancel did not stop active generation", sawToken, err)
	}
	if time.Since(start) > 30*time.Second {
		t.Fatal("cancellation took too long")
	}
	m.mu.Lock()
	leases := 0
	for _, count := range m.leases {
		leases += count
	}
	m.mu.Unlock()
	if leases != 0 {
		t.Fatal("model pair remained leased", leases)
	}
	t.Logf("active generation cancelled and both leases released after %.3fs total", time.Since(start).Seconds())
	metadata, _ := os.CreateTemp(t.TempDir(), "meta")
	defer metadata.Close()
	json.NewEncoder(metadata).Encode(VisionMetadata{SchemaVersion: 1, Question: "What is visible?", ContextTokens: 512, Width: 256, Height: 256})
	mi, _ := OpenInput(metadata, "text", NewID())
	ii, _ := OpenInput(f, "image", NewID())
	wire := contracts.JobsWorkerRequest{ProtocolVersion: 1, RequestId: contracts.DomainId(NewID()), JobId: contracts.DomainId(NewID()), LeaseToken: contracts.DomainId(NewID()), CancellationId: contracts.DomainId(NewID()), Operation: "image_generate", ModelPackId: "01990000-0000-7000-8000-000000000107", Limits: contracts.JobsResourceLimits{Threads: 4, MemoryBytes: s.MemoryBudget(), TimeoutMs: 300000, MaxOutputBytes: 128 << 10, MaxTokens: 512}}
	_, err = s.Run(context.Background(), Request{Wire: wire, Inputs: []Input{mi, ii}, Priority: PriorityChat}, nil)
	if err == nil || !strings.Contains(err.Error(), "exact context budget") {
		t.Fatal("did not enforce actual image tokens before decode", err)
	}
	t.Log("exact multimodal token budget rejects input plus output overflow")
}

func TestVisionPairedAcquisitionResumeAndReadyStatus(t *testing.T) {
	model := append([]byte("GGUF"), bytes.Repeat([]byte("model-"), 12000)...)
	projector := append([]byte("GGUF"), bytes.Repeat([]byte("projector-"), 12000)...)
	p := visionFixture(model, projector)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := model
		if r.URL.Path == "/projector" {
			data = projector
		}
		w.Header().Set("ETag", "\"vision-fixture\"")
		start := 0
		if h := r.Header.Get("Range"); h != "" {
			fmt.Sscanf(h, "bytes=%d-", &start)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
			w.WriteHeader(206)
		}
		w.Write(data[start:])
	}))
	defer server.Close()
	p.URL = server.URL + "/model"
	p.Projector.URL = server.URL + "/projector"
	root := t.TempDir()
	m, _ := NewManager(root)
	m.client = server.Client()
	ctx, cancel := context.WithCancel(context.Background())
	if err := m.Acquire(ctx, p, func(n int64) {
		if n > 20000 {
			cancel()
		}
	}); err == nil {
		t.Fatal("cancelled pair transfer completed")
	}
	cancel()
	m, _ = NewManager(root)
	m.client = server.Client()
	if err := m.Acquire(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.Acquire(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	states, err := m.List()
	if err != nil || len(states) != 1 || states[0].State != "ready" || states[0].DownloadedBytes != p.DownloadBytes() {
		t.Fatal(states, err)
	}
}

func TestVisionRejectsOversizedImageBeforeDecode(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "oversized")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = f.Truncate((20 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = NormalizeVisionImage(context.Background(), f); err == nil || !strings.Contains(err.Error(), "20 MiB") {
		t.Fatal(err)
	}
	// A valid PNG header is enough to reject extreme dimensions without allocating pixels.
	var header bytes.Buffer
	header.WriteString("\x89PNG\r\n\x1a\n")
	binary.Write(&header, binary.BigEndian, uint32(13))
	chunk := append([]byte("IHDR"), make([]byte, 13)...)
	binary.BigEndian.PutUint32(chunk[4:8], 100000)
	binary.BigEndian.PutUint32(chunk[8:12], 100000)
	chunk[12], chunk[13] = 8, 6
	header.Write(chunk)
	binary.Write(&header, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	if err = f.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt(header.Bytes(), 0); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = NormalizeVisionImage(context.Background(), f); err == nil || !strings.Contains(err.Error(), "20 megapixels") {
		t.Fatal(err)
	}
}
func TestVisionQuestionBoundPrecedesPixelShortcut(t *testing.T) {
	s := &Supervisor{}
	for _, q := range []string{"", "   ", strings.Repeat("a", 2049), string([]byte{0xff})} {
		if _, err := s.GenerateImage(context.Background(), "", q, nil, nil); err == nil || !strings.Contains(err.Error(), "image question") {
			t.Fatal(err)
		}
	}
}
