package native

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestPortableCaptureAcknowledgedPauseResumeStop(t *testing.T) {
	s := portableMicrophoneFixture(t)
	inputs, err := s.AudioInputs(context.Background())
	if err != nil || len(inputs) != 1 || inputs[0].ID != "fixture-input" {
		t.Fatalf("inputs: %+v %v", inputs, err)
	}
	f, cleanup, err := NewPrivateStagingFile()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	controller := NewCaptureController()
	started := make(chan struct{}, 1)
	done := make(chan error, 1)
	var result CaptureResult
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		var err error
		result, err = s.Capture(ctx, CaptureRequest{RequestID: NewID(), DurationMS: 500, MaxBytes: 32000, Synthetic: true, InputDeviceID: "fixture-input", Controller: controller}, f, func(raw json.RawMessage) {
			var e struct{ Kind string }
			json.Unmarshal(raw, &e)
			if e.Kind == "progress" {
				select {
				case started <- struct{}{}:
				default:
				}
			}
		})
		done <- err
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	pause, err := controller.Command(ctx, "pause")
	if err != nil {
		t.Fatal(err)
	}
	if pause.State != "paused" || pause.Bytes == 0 || pause.CapturedSamples != pause.Bytes/4 {
		t.Fatal(pause)
	}
	time.Sleep(650 * time.Millisecond) // More than the entire requested audio duration.
	stat, err := f.Stat()
	if err != nil || stat.Size() != pause.Bytes {
		t.Fatalf("paused capture wrote samples: %v %v", stat, err)
	}
	repeated, err := controller.Command(ctx, "pause")
	if err != nil || repeated.Bytes != pause.Bytes {
		t.Fatal(repeated, err)
	}
	resume, err := controller.Command(ctx, "resume")
	if err != nil || resume.Bytes != pause.Bytes {
		t.Fatal(resume, err)
	}
	stop, err := controller.Command(ctx, "stop")
	if err != nil || stop.State != "stopped" {
		t.Fatal(stop, err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if result.Cancelled || result.Bytes != stop.Bytes || result.Bytes > 32000 || result.Bytes < resume.Bytes {
		t.Fatalf("invalid result: %+v %+v", result, stop)
	}
	if _, err = controller.Command(ctx, "resume"); err == nil {
		t.Fatal("finished capture accepted resume")
	}
}

func TestPortableCaptureRejectsMissingSelectedInput(t *testing.T) {
	s := portableMicrophoneFixture(t)
	f, cleanup, err := NewPrivateStagingFile()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err = s.Capture(context.Background(), CaptureRequest{RequestID: NewID(), DurationMS: 10, MaxBytes: 640, Synthetic: true, InputDeviceID: "disconnected-input"}, f, nil); err == nil {
		t.Fatal("missing input silently fell back")
	}
	stat, _ := f.Stat()
	if stat.Size() != 0 {
		t.Fatal("unexpected capture")
	}
}

func TestCaptureControllerRejectsWrongReceiptAndBoundsWait(t *testing.T) {
	c := NewCaptureController()
	defer c.finish()
	if c.acknowledge(CaptureControlAck{ControlID: NewID(), Action: "pause", State: "paused"}) {
		t.Fatal("unknown control accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Command(ctx, "pause"); err == nil {
		t.Fatal("cancelled wait accepted")
	}
}
