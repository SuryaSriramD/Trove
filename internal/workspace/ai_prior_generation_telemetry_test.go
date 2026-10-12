package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

// This test-only recorder snapshots the actual preparation request. Each entry
// into the native generation call is counted even when it returns an error or
// is cancelled. Tokenization and preparation do not count as generation.
type aiEvaluationGenerationControls struct {
	request  AIRequest
	attempts int
}

func newAIEvaluationGenerationControls(request AIRequest) *aiEvaluationGenerationControls {
	return &aiEvaluationGenerationControls{request: request}
}

func (c *aiEvaluationGenerationControls) generate(nativeEntry func() (string, error)) (string, error) {
	c.attempts++
	return nativeEntry()
}

func (c *aiEvaluationGenerationControls) report() map[string]any {
	return aiEvaluationRequestReport(c.request, c.attempts, map[string]any{"phase": "generation-controls"})
}

// Defer before preparation so zero generation entries remain explicit on
// preparation failure. Existing answer and canonical reports stay separate.
func (c *aiEvaluationGenerationControls) log(t *testing.T) {
	t.Helper()
	data, err := json.Marshal(c.report())
	if err != nil {
		t.Errorf("encode actual generation controls: %v", err)
		return
	}
	t.Log(string(data))
}

func TestAIPriorGenerationControlsActualRequestAndEntries(t *testing.T) {
	for _, useMemory := range []bool{false, true} {
		for _, wantedEntries := range []int{0, 1, 2} {
			t.Run(fmt.Sprintf("memory=%t/entries=%d", useMemory, wantedEntries), func(t *testing.T) {
				request := AIRequest{Mode: "rewrite", UseMemory: useMemory, ModelPackID: "actual-native-pack"}
				controls := newAIEvaluationGenerationControls(request)
				// A later loop/request mutation must not rewrite captured controls.
				request.UseMemory = !useMemory
				request.ModelPackID = "later-pack"
				entered := 0
				for i := 0; i < wantedEntries; i++ {
					text, err := controls.generate(func() (string, error) {
						entered++
						return "preserved raw draft", nil
					})
					if text != "preserved raw draft" || err != nil {
						t.Fatalf("generation result changed: %q %v", text, err)
					}
				}
				data, err := json.Marshal(controls.report())
				if err != nil {
					t.Fatal(err)
				}
				decoder := json.NewDecoder(bytes.NewReader(data))
				decoder.UseNumber()
				var report map[string]any
				if err := decoder.Decode(&report); err != nil {
					t.Fatal(err)
				}
				memory, isBoolean := report["useMemory"].(bool)
				attempts, isNumber := report["generationAttempts"].(json.Number)
				if !isBoolean || memory != useMemory || !isNumber || string(attempts) != fmt.Sprint(entered) || entered != wantedEntries || report["modelPackId"] != "actual-native-pack" || report["phase"] != "generation-controls" || len(report) != 4 {
					t.Fatalf("actual typed generation controls lost: %s (entries=%d)", data, entered)
				}
			})
		}
	}
}

func TestAIPriorGenerationControlsErrorAndCancellationAreCounted(t *testing.T) {
	for _, nativeError := range []error{errors.New("native failure"), context.Canceled} {
		t.Run(nativeError.Error(), func(t *testing.T) {
			controls := newAIEvaluationGenerationControls(AIRequest{UseMemory: true, ModelPackID: "actual-native-pack"})
			entered := 0
			text, err := controls.generate(func() (string, error) {
				entered++
				return "partial raw output", nativeError
			})
			if entered != 1 || controls.report()["generationAttempts"] != 1 || text != "partial raw output" || err != nativeError {
				t.Fatalf("native failure was changed, retried, or not counted: entered=%d text=%q err=%v", entered, text, err)
			}
		})
	}
}

// The fresh consumer is an explicit input. Historical consumers remain usable
// for old receipts and must not be silently replaced by these stricter checks.
func TestAIPriorGenerationControlsConsumerContract(t *testing.T) {
	consumer := os.Getenv("TROVE_PRIOR_TELEMETRY_CONSUMER")
	if consumer == "" {
		t.Skip("requires the explicitly selected fresh prior21 telemetry consumer")
	}
	python := os.Getenv("TROVE_EVALUATION_TEST_PYTHON")
	if python == "" {
		var err error
		python, err = exec.LookPath("python3")
		if err != nil {
			t.Fatal("Python required for selected prior21 telemetry consumer")
		}
	}
	for _, useMemory := range []bool{false, true} {
		for _, entries := range []int{0, 1, 2} {
			t.Run(fmt.Sprintf("memory=%t/entries=%d", useMemory, entries), func(t *testing.T) {
				request := AIRequest{UseMemory: useMemory, ModelPackID: "actual-native-pack"}
				controls := newAIEvaluationGenerationControls(request)
				for i := 0; i < entries; i++ {
					_, _ = controls.generate(func() (string, error) { return "partial raw output", context.Canceled })
				}
				payload, err := json.Marshal(map[string]any{"report": controls.report(), "expectedUseMemory": request.UseMemory, "expectedPack": request.ModelPackID, "expectedEntries": entries})
				if err != nil {
					t.Fatal(err)
				}
				program := `import importlib.util,json,sys
spec=importlib.util.spec_from_file_location('prior21_telemetry',sys.argv[1])
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
x=json.load(sys.stdin);report=x['report']
check=lambda value:m.validate_control_report(value,x['expectedUseMemory'],x['expectedPack'])
assert (not check(report)) == (x['expectedEntries']==1)
for key in ('phase','useMemory','generationAttempts','modelPackId'):
 changed=dict(report);changed.pop(key)
 assert check(changed),key
for value in (None,0,1,'false',not x['expectedUseMemory']):
 assert check(dict(report,useMemory=value)),repr(value)
for value in (None,False,True,0,2,'1',1.0):
 assert check(dict(report,generationAttempts=value)),repr(value)
assert check(dict(report,modelPackId='wrong-pack'))
assert check(dict(report,unexpectedField=True))
print('actual Go controls checked; invalid telemetry rejected')
`
				command := exec.Command(python, "-B", "-c", program, consumer)
				command.Stdin = bytes.NewReader(payload)
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("actual prior21 producer-to-consumer contract failed: %v\n%s", err, output)
				}
			})
		}
	}
}
