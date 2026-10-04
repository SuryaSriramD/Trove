package native

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
	"trove.local/phase0/contracts"
)

func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	ms := uint64(time.Now().UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	b[6] = b[6]&15 | 0x70
	b[8] = b[8]&63 | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// QualificationSmoke uses real installed packs and the pinned, public-domain JFK
// fixture. It never acquires models or records microphone input.
func (s *Supervisor) QualificationSmoke(ctx context.Context) (map[string]any, error) {
	results := map[string]any{}
	catalog, e := LoadCatalog(s.bundle)
	if e != nil {
		return nil, e
	}
	dir, e := os.MkdirTemp("", "trove-native-smoke-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	run := func(p Pack, operation string, metadata any, audio []byte) (json.RawMessage, error) {
		meta, _ := json.Marshal(metadata)
		path := filepath.Join(dir, NewID())
		if e := os.WriteFile(path, meta, 0600); e != nil {
			return nil, e
		}
		f, e := os.Open(path)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		input, e := OpenInput(f, "text", NewID())
		if e != nil {
			return nil, e
		}
		inputs := []Input{input}
		if audio != nil {
			ap := filepath.Join(dir, NewID())
			if e = os.WriteFile(ap, audio, 0600); e != nil {
				return nil, e
			}
			af, e := os.Open(ap)
			if e != nil {
				return nil, e
			}
			defer af.Close()
			ai, e := OpenInput(af, "audio", NewID())
			if e != nil {
				return nil, e
			}
			inputs = append(inputs, ai)
		}
		wire := contracts.JobsWorkerRequest{ProtocolVersion: 1, RequestId: contracts.DomainId(NewID()), JobId: contracts.DomainId(NewID()), LeaseToken: contracts.DomainId(NewID()), CancellationId: contracts.DomainId(NewID()), Operation: operation, ModelPackId: contracts.DomainId(p.ID), Limits: contracts.JobsResourceLimits{Threads: 2, MemoryBytes: 1 << 30, TimeoutMs: 120000, MaxOutputBytes: 4 << 20, MaxTokens: 16}}
		return s.Run(ctx, Request{wire, inputs, PriorityChat}, nil)
	}
	for _, p := range catalog {
		// The smoke includes the supported embedding and speech packs. Only
		// the tiny chat fixture is runtime-only; instruction quality has its own
		// source-grounded acceptance tests.
		if p.Kind == "chat" && !p.QualificationOnly {
			continue
		}
		switch p.Kind {
		case "chat":
			raw, e := run(p, "generate", map[string]any{"schemaVersion": 1, "text": "Once upon a time", "contextTokens": 128, "gpuLayers": 0}, nil)
			if e != nil {
				return nil, fmt.Errorf("chat: %w", e)
			}
			var b map[string]any
			_ = json.Unmarshal(raw, &b)
			if b["text"] == "" {
				return nil, fmt.Errorf("empty model generation")
			}
			results["chat"] = b
		case "embedding":
			raw, e := run(p, "embed", map[string]any{"schemaVersion": 1, "texts": []string{"Trove keeps workspace content local."}, "locations": []map[string]any{{"pageId": NewID(), "sourceRevision": "1"}}, "contextTokens": 128, "gpuLayers": 0}, nil)
			if e != nil {
				return nil, fmt.Errorf("embedding: %w", e)
			}
			var b struct {
				Dimension int         `json:"dimension"`
				Vectors   [][]float64 `json:"vectors"`
			}
			_ = json.Unmarshal(raw, &b)
			results["embedding"] = map[string]any{"dimension": b.Dimension, "vectors": len(b.Vectors)}
		case "speech":
			wav, e := os.ReadFile(filepath.Join(s.bundle, "fixtures", "jfk.wav"))
			if e != nil {
				return nil, e
			}
			pcm, e := wavPCM(wav)
			if e != nil {
				return nil, e
			}
			ids := []map[string]string{}
			for i := 0; i < 256; i++ {
				ids = append(ids, map[string]string{"segmentId": NewID(), "blockId": NewID()})
			}
			raw, e := run(p, "transcribe", map[string]any{"schemaVersion": 1, "meetingId": NewID(), "segmentIds": ids, "language": "en", "useGPU": false}, pcm)
			if e != nil {
				return nil, fmt.Errorf("speech: %w", e)
			}
			var b map[string]any
			_ = json.Unmarshal(raw, &b)
			results["speech"] = b
		}
	}
	f, e := os.CreateTemp(dir, "synthetic-audio-")
	if e != nil {
		return nil, e
	}
	defer f.Close()
	capture, e := s.Capture(ctx, CaptureRequest{RequestID: NewID(), DurationMS: 100, MaxBytes: 6400, Synthetic: true}, f, nil)
	if e != nil {
		return nil, e
	}
	results["microphoneSynthetic"] = capture
	return results, nil
}
func wavPCM(wav []byte) ([]byte, error) {
	if len(wav) < 44 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return nil, fmt.Errorf("invalid WAV fixture")
	}
	for pos := 12; pos+8 <= len(wav); {
		n := int(binary.LittleEndian.Uint32(wav[pos+4 : pos+8]))
		if pos+8+n > len(wav) {
			return nil, fmt.Errorf("truncated WAV")
		}
		if string(wav[pos:pos+4]) == "fmt " {
			d := wav[pos+8 : pos+8+n]
			if n < 16 || binary.LittleEndian.Uint16(d) != 1 || binary.LittleEndian.Uint16(d[2:]) != 1 || binary.LittleEndian.Uint32(d[4:]) != 16000 || binary.LittleEndian.Uint16(d[14:]) != 16 {
				return nil, fmt.Errorf("fixture must be PCM16 mono16k")
			}
		}
		if string(wav[pos:pos+4]) == "data" {
			d := wav[pos+8 : pos+8+n]
			out := make([]byte, n*2)
			for i := 0; i+1 < len(d); i += 2 {
				v := float32(int16(binary.LittleEndian.Uint16(d[i:]))) / 32768
				binary.LittleEndian.PutUint32(out[i*2:], math.Float32bits(v))
			}
			return out, nil
		}
		pos += 8 + n + (n % 2)
	}
	return nil, fmt.Errorf("WAV data missing")
}
