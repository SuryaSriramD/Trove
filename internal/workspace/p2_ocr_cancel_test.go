package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"trove.local/phase0/contracts"
)

func ocrCancelFixture(t *testing.T) (*Kernel, contracts.ServiceExecutionContext, string) {
	t.Helper()
	k, e := testKernel(t)
	page := createPage(t, k, e, "OCR cancellation source")
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	file, _ := attachTestFile(t, k, e, page, stageTestFile(t, k, e, b.String()), true)
	return k, e, file
}
func TestP2OCRCancellationPreservesPriorExtraction(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(map[bool]string{false: "helper_running", true: "writer_waiting"}[queued], func(t *testing.T) {
			k, e, file := ocrCancelFixture(t)
			k.SetOCRProvider(func(context.Context, *os.File) (OCRResult, error) {
				return OCRResult{Lines: []OCRLine{{Text: "Retained earlier text", Confidence: 1}}}, nil
			})
			testOK(t, testDispatch(t, k, e, "ocr.run", recoveryRequest(k, map[string]any{"fileId": file})))
			entered := make(chan struct{})
			release := make(chan struct{})
			k.SetOCRProvider(func(ctx context.Context, _ *os.File) (OCRResult, error) {
				close(entered)
				if queued {
					<-release
				} else {
					select {
					case <-ctx.Done():
					case <-release:
					}
				}
				return OCRResult{Lines: []OCRLine{{Text: "Cancelled replacement", Confidence: 1}}}, nil
			})
			request := recoveryRequest(k, map[string]any{"fileId": file})
			runID := str(request["requestId"])
			if queued {
				k.writer <- struct{}{}
			}
			result := make(chan map[string]any, 1)
			go func() { result <- testDispatch(t, k, e, "ocr.run", request) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("helper not entered")
			}
			if queued {
				close(release)
				deadline := time.Now().Add(time.Second)
				for len(k.pending) == 0 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if len(k.pending) == 0 {
					<-k.writer
					t.Fatal("OCR never reached publication queue")
				}
			}
			response := testDispatch(t, k, e, "ocr.cancel", recoveryRequest(k, map[string]any{"runRequestId": runID}))
			if queued {
				<-k.writer
			} else {
				close(release)
			}
			testOK(t, response)
			select {
			case r := <-result:
				testCode(t, r, "CANCELLED")
			case <-time.After(time.Second):
				t.Fatal("OCR did not stop")
			}
			var text string
			if err := k.db.QueryRow("SELECT body FROM document_sections WHERE file_id=?", file).Scan(&text); err != nil || text != "Retained earlier text" {
				t.Fatal(text, err)
			}
		})
	}
}
func TestP2OCREarlyCancellationAndGrantIsolation(t *testing.T) {
	k, e, file := ocrCancelFixture(t)
	called := false
	k.SetOCRProvider(func(context.Context, *os.File) (OCRResult, error) { called = true; return OCRResult{}, nil })
	req := recoveryRequest(k, map[string]any{"fileId": file})
	id := str(req["requestId"])
	testOK(t, testDispatch(t, k, e, "ocr.cancel", recoveryRequest(k, map[string]any{"runRequestId": id})))
	testCode(t, testDispatch(t, k, e, "ocr.run", req), "CANCELLED")
	if called {
		t.Fatal("pre-cancelled operation invoked helper")
	}
	// A new grant cannot consume or cancel another grant's pending request.
	other := k.Grant(e.Actor, "user", e.Scope)
	testOK(t, testDispatch(t, k, other, "ocr.run", req))
	if !called {
		t.Fatal("unrelated grant cancellation crossed scope")
	}
}

func TestP2OCRCancellationBoundedExpiryAndProviderRemoval(t *testing.T) {
	k, e, _ := ocrCancelFixture(t)
	k.SetOCRProvider(func(context.Context, *os.File) (OCRResult, error) { return OCRResult{}, nil })
	value, _ := ocrProviders.Load(k)
	r := value.(*ocrRuntime)
	for i := 0; i < 128; i++ {
		testOK(t, testDispatch(t, k, e, "ocr.cancel", recoveryRequest(k, map[string]any{"runRequestId": NewID()})))
	}
	testCode(t, testDispatch(t, k, e, "ocr.cancel", recoveryRequest(k, map[string]any{"runRequestId": NewID()})), "RESOURCE_EXHAUSTED")
	r.mu.Lock()
	for key := range r.cancelled {
		r.cancelled[key] = time.Now().Add(-time.Second)
	}
	r.mu.Unlock()
	testOK(t, testDispatch(t, k, e, "ocr.cancel", recoveryRequest(k, map[string]any{"runRequestId": NewID()})))
	flow, finish, err := r.begin(context.Background(), e, NewID())
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	k.SetOCRProvider(nil)
	select {
	case <-flow.Done():
	case <-time.After(time.Second):
		t.Fatal("removed helper retained active flight")
	}
	testCode(t, testDispatch(t, k, e, "ocr.cancel", recoveryRequest(k, map[string]any{"runRequestId": NewID()})), "WORKER_FAILED")
}

func TestP2OCRWrongGrantCannotCancelRunningRequest(t *testing.T) {
	k, e, file := ocrCancelFixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	k.SetOCRProvider(func(ctx context.Context, _ *os.File) (OCRResult, error) {
		close(entered)
		select {
		case <-ctx.Done():
			return OCRResult{}, ctx.Err()
		case <-release:
		}
		return OCRResult{Lines: []OCRLine{{Text: "Authorized result", Confidence: 1}}}, nil
	})
	req := recoveryRequest(k, map[string]any{"fileId": file})
	out := make(chan map[string]any, 1)
	go func() { out <- testDispatch(t, k, e, "ocr.run", req) }()
	<-entered
	other := k.Grant(e.Actor, "user", e.Scope)
	external := k.Grant(contracts.ServiceActor{Kind: "external_client", ActorId: contracts.DomainId(NewID())}, "external", e.Scope)
	testCode(t, testDispatch(t, k, external, "ocr.cancel", recoveryRequest(k, map[string]any{"runRequestId": req["requestId"]})), "SCOPE_DENIED")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	var response map[string]any
	if err := json.Unmarshal(k.Dispatch(cancelled, e, "ocr.cancel", marshal(recoveryRequest(k, map[string]any{"runRequestId": req["requestId"]}))), &response); err != nil {
		t.Fatal(err)
	}
	testCode(t, response, "CANCELLED")
	testOK(t, testDispatch(t, k, other, "ocr.cancel", recoveryRequest(k, map[string]any{"runRequestId": req["requestId"]})))
	close(release)
	testOK(t, <-out)
}

func TestP2OCRLifecycleSuspensionPreventsRacingAdmission(t *testing.T) {
	k, e, _ := ocrCancelFixture(t)
	k.SetOCRProvider(func(context.Context, *os.File) (OCRResult, error) { return OCRResult{}, nil })
	value, _ := ocrProviders.Load(k)
	r := value.(*ocrRuntime)
	flow, finish, err := r.begin(context.Background(), e, NewID())
	if err != nil {
		t.Fatal(err)
	}
	resume := k.SuspendOCR()
	defer resume()
	if flow.Err() == nil {
		t.Fatal("lifecycle did not cancel running read")
	}
	finish()
	if _, _, err = r.begin(context.Background(), e, NewID()); err == nil {
		t.Fatal("new extraction admitted during lifecycle transition")
	}
	resume()
	resume()
	_, finish, err = r.begin(context.Background(), e, NewID())
	if err != nil {
		t.Fatal("failed transition could not resume OCR", err)
	}
	finish()
}
