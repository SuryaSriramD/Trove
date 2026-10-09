package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Bind evaluation controls to the request actually passed to preparation, not
// to a separately copied corpus value. False and zero remain explicit evidence.
func aiEvaluationRequestReport(request AIRequest, attempts int, fields map[string]any) map[string]any {
	report := make(map[string]any, len(fields)+3)
	for key, value := range fields {
		report[key] = value
	}
	report["useMemory"] = request.UseMemory
	report["modelPackId"] = request.ModelPackID
	report["generationAttempts"] = attempts
	return report
}

func TestAIEvaluationRequestReportConsumerContract(t *testing.T) {
	python := os.Getenv("TROVE_EVALUATION_TEST_PYTHON")
	if python == "" {
		var err error
		python, err = exec.LookPath("python3")
		if err != nil {
			t.Skip("requires Python to exercise the actual evaluation-report consumer")
		}
	}
	consumer, err := filepath.Abs("../../scripts/run-expanded-evaluation.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, useMemory := range []bool{false, true} {
		t.Run(fmt.Sprint(useMemory), func(t *testing.T) {
			request := AIRequest{Mode: "answer", Prompt: "What is the current region? Give one factual sentence without a heading.", UseMemory: useMemory, ModelPackID: NewID()}
			prepared, err := prepareAIContext(context.Background(), request, []AIEvidence{{Title: "Current region", Text: "The current region is North."}}, nil, "", wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			attempts := 0
			_, err = GeneratePreparedAI(context.Background(), prepared, func(context.Context, string, string, int, string, func(string)) (string, error) {
				attempts++
				return `{"title":"","claims":[{"text":"The current region is North.","quote":"The current region is North."}]}`, nil
			}, nil)
			if err != nil || attempts != 1 || prepared.Run.Request != request {
				t.Fatalf("actual prepared request or generation changed: attempts=%d error=%v", attempts, err)
			}
			fields := map[string]any{"fixture": "contract-case", "corpusSHA256": "locked-corpus", "useMemory": !useMemory, "modelPackId": "wrong-pack", "generationAttempts": 99}
			report := aiEvaluationRequestReport(request, attempts, fields)
			payload, err := json.Marshal(map[string]any{"report": report, "expected": map[string]any{"useMemory": useMemory, "modelPackId": request.ModelPackID, "corpusSHA256": "locked-corpus"}})
			if err != nil {
				t.Fatal(err)
			}
			// The production Python consumer reads the actual Go-produced bytes.
			// Its negative controls also ensure that missing/false are distinct,
			// numeric booleans do not pass, and attempts are not inferred from pass.
			program := `import importlib.util,json,sys
spec=importlib.util.spec_from_file_location('evaluation',sys.argv[1])
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
x=json.load(sys.stdin);report=x['report'];expected=x['expected']
assert m.validate_request_report(report,expected) is None
for key in ('useMemory','modelPackId','corpusSHA256','generationAttempts'):
 changed=dict(report);changed.pop(key)
 assert m.validate_request_report(changed,expected) is not None,key
for value in (None,0,1,'false',not expected['useMemory']):
 changed=dict(report,useMemory=value)
 assert m.validate_request_report(changed,expected) is not None,repr(value)
for value in (None,False,True,0,2,'1',1.0):
 changed=dict(report,generationAttempts=value)
 assert m.validate_request_report(changed,expected) is not None,repr(value)
print('actual Go report accepted; corrupted controls rejected')
`
			command := exec.Command(python, "-B", "-c", program, consumer)
			command.Stdin = bytes.NewReader(payload)
			if out, err := command.CombinedOutput(); err != nil {
				t.Fatalf("producer-to-consumer contract failed: %v\n%s", err, out)
			}
		})
	}
}

func TestAIEvaluationRequestReportRetainsUnsuccessfulAttemptCounts(t *testing.T) {
	request := AIRequest{UseMemory: true, ModelPackID: "actual-pack"}
	for _, attempts := range []int{0, 2} {
		report := aiEvaluationRequestReport(request, attempts, map[string]any{"passed": false})
		if report["generationAttempts"] != attempts || report["useMemory"] != true || report["modelPackId"] != request.ModelPackID {
			t.Fatal("failure or pre-generation report lost its actual controls", report)
		}
	}
}
