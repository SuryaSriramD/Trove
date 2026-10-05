package native

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

var (
	ErrAudioUnsupported = errors.New("UNSUPPORTED_FORMAT")
	ErrAudioCorrupt     = errors.New("CORRUPT_AUDIO")
	ErrAudioLimit       = errors.New("AUDIO_LIMIT")
)

const audioRuntimeCommit = "ffmpeg-9.0.2-trove-restricted-v1"

const maximumDecodedAudioBytes int64 = 16000 * 4 * 7200

type AudioDecodeResult struct {
	SchemaVersion    int     `json:"schemaVersion"`
	Frames           int64   `json:"frames"`
	Bytes            int64   `json:"bytes"`
	SampleRate       int     `json:"sampleRate"`
	Channels         int     `json:"channels"`
	Format           string  `json:"format"`
	Container        string  `json:"container"`
	SourceSampleRate float64 `json:"sourceSampleRate"`
	SourceChannels   int     `json:"sourceChannels"`
	Code             string  `json:"code,omitempty"`
	Message          string  `json:"message,omitempty"`
}

type decodeOutput struct{ bytes.Buffer }

func (b *decodeOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4096 {
		return 0, ErrAudioLimit
	}
	return b.Buffer.Write(p)
}

func (s *Supervisor) audioDecoder() (Helper, error) {
	var record struct {
		Helper
		SchemaVersion int    `json:"schemaVersion"`
		Platform      string `json:"platform"`
	}
	if s == nil || !supportedNativeTarget(runtime.GOOS, runtime.GOARCH) {
		return record.Helper, fmt.Errorf("%w: common-format decoding requires a supported verified helper", ErrAudioUnsupported)
	}
	raw, err := os.ReadFile(filepath.Join(s.bundle, "audio-decoder.json"))
	if err != nil {
		return record.Helper, fmt.Errorf("%w: bundled audio decoder is unavailable; original recording is retained", ErrAudioUnsupported)
	}
	if json.Unmarshal(raw, &record) != nil {
		return record.Helper, errors.New("invalid audio decoder manifest")
	}
	legacy := runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" &&
		record.Runtime == "CoreAudio" && record.RuntimeCommit == "trove-audio-decoder-v1"
	if record.SchemaVersion != 1 || record.Platform != runtime.GOOS || record.Name != "audio-decoder" ||
		record.Architecture != runtime.GOARCH || !digestPattern.MatchString(record.SHA256) ||
		(!legacy && (record.Runtime != "FFmpeg" || record.RuntimeCommit != audioRuntimeCommit)) {
		return record.Helper, errors.New("invalid audio decoder manifest")
	}
	return record.Helper, s.verifyHelper(record.Helper)
}

// DecodeAudio normalizes a private snapshot of an authorized descriptor. It does
// not alter the canonical asset. The caller must discard staging output on error.
// Decoder availability/codecs are platform dependent, never a cross-platform claim.
func (s *Supervisor) DecodeAudio(ctx context.Context, source, destination *os.File) (AudioDecodeResult, error) {
	var result AudioDecodeResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	helper, err := s.audioDecoder()
	if err != nil {
		return result, err
	}
	if source == nil || destination == nil {
		return result, fmt.Errorf("%w: missing audio descriptor", ErrAudioCorrupt)
	}
	input, err := source.Stat()
	if err != nil || !input.Mode().IsRegular() || input.Size() <= 0 {
		return result, fmt.Errorf("%w: empty or invalid recording", ErrAudioCorrupt)
	}
	if input.Size() > 512<<20 {
		return result, fmt.Errorf("%w: recording exceeds 512 MiB", ErrAudioLimit)
	}
	output, err := destination.Stat()
	if err != nil || !output.Mode().IsRegular() || output.Size() != 0 || os.SameFile(input, output) {
		return result, errors.New("audio decoder requires an empty separate staging file")
	}
	run, release, err := s.Scheduler.Enter(ctx, PriorityTranscription)
	if err != nil {
		return result, err
	}
	defer release()
	run, timeout := context.WithTimeout(run, 3*time.Minute)
	defer timeout()
	run, cancel := context.WithCancelCause(run)
	defer cancel(context.Canceled)
	dir, err := privateWorkerDirectory("trove-decode-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "input")
	staging, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	reader := io.NewSectionReader(source, 0, input.Size())
	buffer := make([]byte, 64<<10)
	var copied int64
	for {
		if err = run.Err(); err != nil {
			break
		}
		var count int
		count, err = reader.Read(buffer)
		if count > 0 {
			if _, writeErr := staging.Write(buffer[:count]); writeErr != nil {
				err = writeErr
				break
			}
			copied += int64(count)
		}
		if err != nil {
			break
		}
	}
	closeErr := staging.Close()
	if err != io.EOF {
		return result, err
	}
	if closeErr != nil {
		return result, closeErr
	}
	if copied != input.Size() {
		return result, fmt.Errorf("%w: source changed during staging", ErrAudioCorrupt)
	}
	snapshot, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer snapshot.Close()
	cmd := exec.CommandContext(run, filepath.Join(s.bundle, helperFilename(helper.Name)))
	cmd.Dir = dir
	cmd.Env = workerEnvironment(dir)
	process, err := prepareWorker(cmd, []*os.File{snapshot, destination}, 512<<20)
	if err != nil {
		return result, err
	}
	defer process.close()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return result, err
	}
	defer stdin.Close()
	stdout := &decodeOutput{}
	cmd.Stdout, cmd.Stderr = stdout, &ringLog{}
	cmd.Cancel = func() error { return process.stop(true) }
	cmd.WaitDelay = 2 * time.Second
	if err = process.start(); err != nil {
		return result, err
	}
	// The child receives no request until Windows Job assignment succeeds.
	if _, err = stdin.Write([]byte("{\"schemaVersion\":1}")); err != nil {
		_ = process.stop(true)
		_ = cmd.Wait()
		if run.Err() != nil {
			return result, context.Cause(run)
		}
		return result, err
	}
	_ = stdin.Close()
	stopped := make(chan struct{})
	var watcher sync.WaitGroup
	watcher.Add(1)
	defer func() { close(stopped); watcher.Wait() }()
	go func() {
		defer watcher.Done()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-run.Done():
				return
			case <-ticker.C:
				if stat, e := destination.Stat(); e == nil && stat.Size() > maximumDecodedAudioBytes {
					cancel(fmt.Errorf("%w: decoded output budget", ErrAudioLimit))
				}
				if rss, e := process.rss(); e == nil && rss > 512<<20 {
					cancel(fmt.Errorf("%w: decoder memory budget", ErrAudioLimit))
				}
			}
		}
	}()
	err = cmd.Wait()
	if run.Err() != nil {
		return result, context.Cause(run)
	}
	if json.Unmarshal(stdout.Bytes(), &result) != nil || result.SchemaVersion != 1 {
		return result, fmt.Errorf("%w: invalid decoder response", ErrAudioCorrupt)
	}
	if result.Code != "" {
		cause := ErrAudioCorrupt
		if result.Code == "UNSUPPORTED_FORMAT" {
			cause = ErrAudioUnsupported
		} else if result.Code == "AUDIO_LIMIT" {
			cause = ErrAudioLimit
		}
		return result, fmt.Errorf("%w: %s; original recording is retained", cause, result.Message)
	}
	if err != nil {
		return result, fmt.Errorf("audio decoder failed: %w", err)
	}
	output, err = destination.Stat()
	if result.Bytes > maximumDecodedAudioBytes {
		return result, fmt.Errorf("%w: decoded duration exceeds two hours", ErrAudioLimit)
	}
	if err != nil || result.Bytes <= 0 || result.Frames <= 0 || result.Frames > maximumDecodedAudioBytes/4 || result.Frames*4 != result.Bytes ||
		output.Size() != result.Bytes || result.SampleRate != 16000 || result.Channels != 1 || result.Format != "f32le" {
		return result, fmt.Errorf("%w: decoder output shape", ErrAudioCorrupt)
	}
	if result.SourceChannels < 1 || result.SourceChannels > 2 || result.SourceSampleRate < 8000 ||
		result.SourceSampleRate > 192000 || math.IsNaN(result.SourceSampleRate) || math.IsInf(result.SourceSampleRate, 0) ||
		(result.Container != "WAV" && result.Container != "MP3" && result.Container != "AAC" && result.Container != "M4A") {
		return result, fmt.Errorf("%w: decoder source shape", ErrAudioCorrupt)
	}
	// Independently validate the derived bytes, including a helper that returns
	// a plausible result envelope but writes NaN, Infinity or clipped samples.
	samples := make([]byte, 64<<10)
	for offset := int64(0); offset < result.Bytes; {
		if run.Err() != nil {
			return result, context.Cause(run)
		}
		count := int64(len(samples))
		if result.Bytes-offset < count {
			count = result.Bytes - offset
		}
		n, e := destination.ReadAt(samples[:count], offset)
		if e != nil || int64(n) != count {
			return result, fmt.Errorf("%w: decoded sample read", ErrAudioCorrupt)
		}
		for at := 0; at < n; at += 4 {
			sample := float64(math.Float32frombits(binary.LittleEndian.Uint32(samples[at:])))
			if math.IsNaN(sample) || math.IsInf(sample, 0) || math.Abs(sample) > 1.01 {
				return result, fmt.Errorf("%w: invalid decoded sample", ErrAudioCorrupt)
			}
		}
		offset += count
	}
	return result, nil
}
