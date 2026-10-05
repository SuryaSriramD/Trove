package native

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The actual C++ helper must reject unbounded/extended requests before touching
// media. Launch through the same descriptor and post-containment barrier on all OSes.
func TestRealAudioDecoderProtocolRejectsPathsAndOversizedRequests(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires real bundled decoder")
	}
	bundle := os.Getenv("TROVE_NATIVE_BUNDLE")
	for _, request := range []string{
		"{\"schemaVersion\":1,\"url\":\"https://127.0.0.1/recording.mp3\"}",
		"{\"schemaVersion\":1,\"path\":\"/etc/passwd\"}",
		"{\"schemaVersion\":2}",
		strings.Repeat(" ", 4097),
	} {
		t.Run(request[:min(len(request), 48)], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, filepath.Join(bundle, helperFilename("audio-decoder")))
			dir, err := privateWorkerDirectory("trove-audio-protocol-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			command.Dir = dir
			command.Env = workerEnvironment(dir)
			input, output := audioTestFile(t, []byte("must not be decoded")), audioTestFile(t, nil)
			process, err := prepareWorker(command, []*os.File{input, output}, 512<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer process.close()
			stdin, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			response := &decodeOutput{}
			command.Stdout = response
			command.Cancel = func() error { return process.stop(true) }
			if err = process.start(); err != nil {
				t.Fatal(err)
			}
			_, _ = stdin.Write([]byte(request))
			stdin.Close()
			err = command.Wait()
			var result AudioDecodeResult
			if err == nil || json.Unmarshal(response.Bytes(), &result) != nil || (result.Code != "CORRUPT_AUDIO" && result.Code != "AUDIO_LIMIT") {
				t.Fatalf("request accepted or invalid error: %v %s", err, response.Bytes())
			}
			stat, _ := output.Stat()
			if stat.Size() != 0 {
				t.Fatal("invalid request wrote derived bytes")
			}
		})
	}
}

func TestRealAudioDecoderRejectsNonfiniteSource(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires real bundled decoder")
	}
	source := make([]byte, 44+16000*4)
	copy(source, "RIFF")
	binary.LittleEndian.PutUint32(source[4:], uint32(len(source)-8))
	copy(source[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(source[16:], 16)
	binary.LittleEndian.PutUint16(source[20:], 3)
	binary.LittleEndian.PutUint16(source[22:], 1)
	binary.LittleEndian.PutUint32(source[24:], 16000)
	binary.LittleEndian.PutUint32(source[28:], 64000)
	binary.LittleEndian.PutUint16(source[32:], 4)
	binary.LittleEndian.PutUint16(source[34:], 32)
	copy(source[36:], "data")
	binary.LittleEndian.PutUint32(source[40:], 16000*4)
	for _, sample := range []float32{float32(math.NaN()), float32(math.Inf(1)), 2.0} {
		binary.LittleEndian.PutUint32(source[44:], math.Float32bits(sample))
		input, output := audioTestFile(t, source), audioTestFile(t, nil)
		s := &Supervisor{bundle: os.Getenv("TROVE_NATIVE_BUNDLE")}
		if _, err := s.DecodeAudio(context.Background(), input, output); !errors.Is(err, ErrAudioCorrupt) {
			t.Fatalf("nonfinite/out-of-range input accepted: %v", err)
		}
	}
}
