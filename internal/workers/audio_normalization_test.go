package workers

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

type normalizerFunc func(context.Context, *os.File, *os.File) (native.AudioDecodeResult, error)

func (f normalizerFunc) DecodeAudio(ctx context.Context, source, destination *os.File) (native.AudioDecodeResult, error) {
	return f(ctx, source, destination)
}

func TestNormalizedAudioUsesPrivateOutputAndPreservesOriginal(t *testing.T) {
	source, err := os.CreateTemp(t.TempDir(), "original-")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	original := []byte("compressed bytes retained canonically")
	if _, err = source.Write(original); err != nil {
		t.Fatal(err)
	}
	var temporary string
	decoder := normalizerFunc(func(ctx context.Context, in, out *os.File) (native.AudioDecodeResult, error) {
		if in != source || in == out {
			t.Fatal("decoder was not given a separate derived destination")
		}
		temporary = out.Name()
		assertPrivateAudioStaging(t, temporary)
		var sample [4]byte
		binary.LittleEndian.PutUint32(sample[:], math.Float32bits(.25))
		_, err := out.Write(sample[:])
		return native.AudioDecodeResult{}, err
	})
	audio, cleanup, err := normalizedInput(context.Background(), source, contracts.DomainFile{OriginalName: "renamed.dat"}, decoder)
	if err != nil || audio == nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err = os.Stat(filepath.Dir(temporary)); !os.IsNotExist(err) {
		t.Fatal("normalized file was not removed")
	}
	actual, _ := os.ReadFile(source.Name())
	if string(actual) != string(original) {
		t.Fatal("original recording changed")
	}
}

func TestNormalizedAudioFailureAndInvalidSamplesRemovePartialOutput(t *testing.T) {
	for _, failure := range []error{context.Canceled, native.ErrAudioCorrupt, native.ErrAudioUnsupported, nil} {
		t.Run(map[error]string{context.Canceled: "cancel", native.ErrAudioCorrupt: "corrupt", native.ErrAudioUnsupported: "unsupported", nil: "invalid-output"}[failure], func(t *testing.T) {
			source, err := os.CreateTemp(t.TempDir(), "original-")
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			source.WriteString("compressed original")
			var temporary string
			decoder := normalizerFunc(func(ctx context.Context, in, out *os.File) (native.AudioDecodeResult, error) {
				temporary = out.Name()
				assertPrivateAudioStaging(t, temporary)
				var sample [4]byte
				binary.LittleEndian.PutUint32(sample[:], math.Float32bits(float32(math.NaN())))
				out.Write(sample[:])
				return native.AudioDecodeResult{}, failure
			})
			_, _, err = normalizedInput(context.Background(), source, contracts.DomainFile{}, decoder)
			if err == nil || (failure != nil && !errors.Is(err, failure)) {
				t.Fatalf("failed normalization was accepted: %v", err)
			}
			if _, err = os.Stat(filepath.Dir(temporary)); !os.IsNotExist(err) {
				t.Fatal("failed normalization leaked a partial output")
			}
			actual, _ := os.ReadFile(source.Name())
			if string(actual) != "compressed original" {
				t.Fatal("failed normalization changed source")
			}
		})
	}
}

func TestNormalizedAudioCancellationBeforeValidation(t *testing.T) {
	source, err := os.CreateTemp(t.TempDir(), "audio-")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = normalizedInput(ctx, source, contracts.DomainFile{OriginalName: "capture.f32le"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled normalization continued: %v", err)
	}
}

type decodedSpeech struct {
	*fakeSpeech
	normalizerFunc
}

func TestNormalizedAudioCanonicalJobRetainsOriginalThroughFailureAndRetry(t *testing.T) {
	ctx := context.Background()
	k, err := workspace.Create(filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	owner := k.OwnerContext()
	path := filepath.Join(t.TempDir(), "meeting.mp3")
	original := []byte("fixture compressed original; codec behavior is tested by the native fixture suite")
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	selection, err := k.RegisterSelection(path)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := k.CreateMeetingFromSelection(ctx, owner, selection, "Retained original")
	if err != nil {
		t.Fatal(err)
	}
	pack := workspace.NewID()
	if _, err = k.QueueMeetingTranscription(ctx, owner, draft.MeetingID, pack); err != nil {
		t.Fatal(err)
	}
	job, err := k.ClaimJob(ctx, []string{"TRANSCRIBE_AUDIO"})
	if err != nil {
		t.Fatal(err)
	}
	failure := native.ErrAudioCorrupt
	speech := &decodedSpeech{fakeSpeech: &fakeSpeech{}, normalizerFunc: func(ctx context.Context, source, out *os.File) (native.AudioDecodeResult, error) {
		if failure != nil {
			return native.AudioDecodeResult{}, failure
		}
		_, err := out.Write(make([]byte, 16000*4))
		return native.AudioDecodeResult{}, err
	}}
	err = transcribeWithRunner(ctx, k, speech, job)
	if !errors.Is(err, native.ErrAudioCorrupt) {
		t.Fatalf("expected recoverable decode error: %v", err)
	}
	if err = k.FinishJob(ctx, string(job.JobId), string(*job.LeaseToken), err, false); err != nil {
		t.Fatal(err)
	}
	assertOriginal := func() {
		t.Helper()
		file, meta, err := k.ReadFile(ctx, owner, draft.FileID)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		actual := make([]byte, len(original))
		if _, err = file.ReadAt(actual, 0); err != nil || string(actual) != string(original) || meta.OriginalName != "meeting.mp3" {
			t.Fatalf("canonical original changed: %v", err)
		}
	}
	assertOriginal()
	if _, err = k.QueueMeetingTranscription(ctx, owner, draft.MeetingID, pack); err != nil {
		t.Fatal(err)
	}
	retry, err := k.ClaimJob(ctx, []string{"TRANSCRIBE_AUDIO"})
	if err != nil || retry.JobId != job.JobId {
		t.Fatalf("decode failure could not be retried: %v", err)
	}
	failure = nil
	if err = transcribeWithRunner(ctx, k, speech, retry); err != nil {
		t.Fatal(err)
	}
	assertOriginal()
	meeting, _, err := k.ReadMeetingForJob(ctx, owner, draft.MeetingID)
	if err != nil || meeting.RecordingFileId == nil || string(*meeting.RecordingFileId) != draft.FileID || speech.calls != 1 {
		t.Fatalf("transcription replaced source identity: %+v %v", meeting, err)
	}
}

func assertPrivateAudioStaging(t *testing.T, path string) {
	t.Helper()
	dir := filepath.Dir(path)
	if dir == os.TempDir() || !strings.HasPrefix(filepath.Base(dir), "trove-staging-") {
		t.Fatal("PCM exposed directly in ambient temp")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
		t.Fatal("PCM directory is not owner-only")
	}
}

func TestNormalizedAudioPCMStagingLifecycle(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "invalid-float"}[invalid], func(t *testing.T) {
			parent := t.TempDir()
			t.Setenv("TMPDIR", parent)
			t.Setenv("TMP", parent)
			t.Setenv("TEMP", parent)
			source, err := os.CreateTemp(t.TempDir(), "source-")
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			header := make([]byte, 48)
			copy(header, "RIFF")
			binary.LittleEndian.PutUint32(header[4:], 40)
			copy(header[8:], "WAVEfmt ")
			binary.LittleEndian.PutUint32(header[16:], 16)
			binary.LittleEndian.PutUint16(header[20:], 3)
			binary.LittleEndian.PutUint16(header[22:], 1)
			binary.LittleEndian.PutUint32(header[24:], 16000)
			binary.LittleEndian.PutUint32(header[28:], 64000)
			binary.LittleEndian.PutUint16(header[32:], 4)
			binary.LittleEndian.PutUint16(header[34:], 32)
			copy(header[36:], "data")
			binary.LittleEndian.PutUint32(header[40:], 4)
			sample := float32(.25)
			if invalid {
				sample = float32(math.NaN())
			}
			binary.LittleEndian.PutUint32(header[44:], math.Float32bits(sample))
			source.Write(header)
			output, cleanup, err := pcmInputContext(context.Background(), source, contracts.DomainFile{OriginalName: "sample.wav"})
			if (err != nil) != invalid {
				t.Fatalf("PCM result: %v", err)
			}
			if !invalid {
				assertPrivateAudioStaging(t, output.Name())
			}
			cleanup()
			paths, _ := filepath.Glob(filepath.Join(parent, "trove-staging-*"))
			if len(paths) != 0 {
				t.Fatalf("PCM staging leaked: %v", paths)
			}
			data, _ := os.ReadFile(source.Name())
			if string(data) != string(header) {
				t.Fatal("source PCM changed")
			}
		})
	}
}
