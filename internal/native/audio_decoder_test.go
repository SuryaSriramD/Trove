package native

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func audioTestBundle(t *testing.T, body string) *Supervisor {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("CoreAudio decoder is explicitly macOS arm64 only")
	}
	bundle := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundle, "audio-decoder"), []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(body))
	record, _ := json.Marshal(map[string]any{"schemaVersion": 1, "name": "audio-decoder", "platform": "darwin",
		"architecture": "arm64", "runtime": "CoreAudio", "runtimeCommit": "trove-audio-decoder-v1", "sha256": hex.EncodeToString(hash[:])})
	if err := os.WriteFile(filepath.Join(bundle, "audio-decoder.json"), record, 0600); err != nil {
		t.Fatal(err)
	}
	return &Supervisor{bundle: bundle}
}

func audioTestFile(t *testing.T, data []byte) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "audio-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	if _, err = file.Write(data); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestAudioDecoderRejectsChangedHelper(t *testing.T) {
	s := audioTestBundle(t, "#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(filepath.Join(s.bundle, "audio-decoder"), []byte("changed"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := s.DecodeAudio(context.Background(), audioTestFile(t, []byte("source")), audioTestFile(t, nil))
	if err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("changed helper accepted: %v", err)
	}
}

func TestAudioDecoderCancellationKeepsOriginalAndRemovesPrivateCopy(t *testing.T) {
	s := audioTestBundle(t, "#!/bin/sh\nexec /bin/sleep 30\n")
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	input := audioTestFile(t, []byte("original audio remains unchanged"))
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := s.DecodeAudio(ctx, input, audioTestFile(t, nil))
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
		t.Fatalf("decoder cancellation was not prompt: %v", err)
	}
	raw, _ := os.ReadFile(input.Name())
	if string(raw) != "original audio remains unchanged" {
		t.Fatal("canonical input changed")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "trove-decode-*"))
	if len(paths) != 0 {
		t.Fatalf("private input snapshots leaked: %v", paths)
	}
}

func TestAudioDecoderLimitsAndSeparateDestination(t *testing.T) {
	s := audioTestBundle(t, "#!/bin/sh\nexit 0\n")
	input := audioTestFile(t, []byte("original"))
	if _, err := s.DecodeAudio(context.Background(), input, input); err == nil {
		t.Fatal("canonical source accepted as decoder destination")
	}
	if err := input.Truncate((512 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecodeAudio(context.Background(), input, audioTestFile(t, nil)); !errors.Is(err, ErrAudioLimit) {
		t.Fatalf("oversized input was accepted: %v", err)
	}
}

func TestAudioDecoderAbsentIsRecoverable(t *testing.T) {
	s := &Supervisor{bundle: t.TempDir()}
	if _, err := s.DecodeAudio(context.Background(), audioTestFile(t, []byte("original")), audioTestFile(t, nil)); !errors.Is(err, ErrAudioUnsupported) {
		t.Fatalf("missing helper was not reported as unsupported: %v", err)
	}
}

func TestAudioDecoderRejectsDeclaredDurationBeyondLimit(t *testing.T) {
	s := audioTestBundle(t, "#!/bin/sh\nprintf '%s' '{\"schemaVersion\":1,\"bytes\":460800004,\"frames\":115200001,\"sampleRate\":16000,\"channels\":1,\"format\":\"f32le\"}'\n")
	if _, err := s.DecodeAudio(context.Background(), audioTestFile(t, []byte("original")), audioTestFile(t, nil)); !errors.Is(err, ErrAudioLimit) {
		t.Fatalf("excessive decoded duration accepted: %v", err)
	}
}

// The committed inputs are synthetic; no microphone permission or model weights
// are used. This proves the actual bundled decoder, not speech recognition quality.
func TestRealAudioDecoderCommonFormats(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires built audio decoder; run with TROVE_NATIVE_INTEGRATION=1")
	}
	bundle := os.Getenv("TROVE_NATIVE_BUNDLE")
	if bundle == "" {
		bundle, _ = filepath.Abs("../../build/native")
	}
	s := &Supervisor{bundle: bundle}
	var manifest struct {
		Files []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	raw, err := os.ReadFile("testdata/audio/manifest.json")
	if err != nil || json.Unmarshal(raw, &manifest) != nil || len(manifest.Files) != 4 {
		t.Fatalf("missing synthetic fixture manifest: %v", err)
	}
	for _, fixture := range manifest.Files {
		t.Run(fixture.Name, func(t *testing.T) {
			original, err := os.ReadFile(filepath.Join("testdata/audio", fixture.Name))
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(original)
			if hex.EncodeToString(hash[:]) != fixture.SHA256 {
				t.Fatal("fixture differs from recorded source")
			}
			// The decoder receives no filename: renaming never establishes format.
			input, output := audioTestFile(t, original), audioTestFile(t, nil)
			result, err := s.DecodeAudio(context.Background(), input, output)
			if err != nil {
				t.Fatal(err)
			}
			if result.Frames < 15000 || result.Frames > 18000 || result.SourceChannels != 2 || result.SourceSampleRate != 48000 {
				t.Fatalf("normalization lost format/duration: %+v", result)
			}
			data, err := io.ReadAll(io.NewSectionReader(output, 0, result.Bytes))
			if err != nil {
				t.Fatal(err)
			}
			energy := 0.0
			for i := 0; i < len(data); i += 4 {
				x := float64(math.Float32frombits(binary.LittleEndian.Uint32(data[i:])))
				if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) > 1.01 {
					t.Fatal("invalid decoded sample")
				}
				energy += x * x
			}
			if energy/float64(result.Frames) < 0.001 {
				t.Fatal("normalization produced silence")
			}
			after, _ := os.ReadFile(input.Name())
			if sha256.Sum256(after) != hash {
				t.Fatal("original source changed")
			}
		})
	}
	t.Run("corrupt-known-container", func(t *testing.T) {
		data := []byte("RIFF\xff\xff\xff\x7fWAVEfmt \x10\x00\x00\x00broken")
		if _, err := s.DecodeAudio(context.Background(), audioTestFile(t, data), audioTestFile(t, nil)); !errors.Is(err, ErrAudioCorrupt) {
			t.Fatalf("corrupt audio was not classified correctly: %v", err)
		}
	})
	t.Run("unsupported", func(t *testing.T) {
		if _, err := s.DecodeAudio(context.Background(), audioTestFile(t, []byte("not audio, even if renamed.mp3")), audioTestFile(t, nil)); !errors.Is(err, ErrAudioUnsupported) {
			t.Fatalf("unsupported input was not classified correctly: %v", err)
		}
	})
	t.Run("unsupported-channel-profile", func(t *testing.T) {
		data, err := os.ReadFile("testdata/audio/tones.wav")
		if err != nil {
			t.Fatal(err)
		}
		binary.LittleEndian.PutUint16(data[22:], 3)
		binary.LittleEndian.PutUint32(data[28:], 48000*3*2)
		binary.LittleEndian.PutUint16(data[32:], 3*2)
		if _, err = s.DecodeAudio(context.Background(), audioTestFile(t, data), audioTestFile(t, nil)); !errors.Is(err, ErrAudioUnsupported) {
			t.Fatalf("unsupported valid WAV profile was not classified correctly: %v", err)
		}
	})
}
