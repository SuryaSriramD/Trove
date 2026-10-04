package contracts

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"sync"
)

//go:embed *.schema.json
var schemas embed.FS
var compilerOnce sync.Once
var compiler *jsonschema.Compiler
var compilerError error
var compileMu sync.Mutex

// Validate must precede typed decoding at every untrusted IPC/import/worker boundary.
// Schema validation is followed by the semantic/service checks in services.md.
func Validate(document, definition string, raw []byte) error {
	compilerOnce.Do(func() {
		compiler = jsonschema.NewCompiler()
		compiler.AssertFormat()
		for _, name := range []string{"domain", "service", "jobs"} {
			b, err := schemas.ReadFile(name + ".schema.json")
			if err != nil {
				compilerError = err
				return
			}
			var value any
			if err = json.Unmarshal(b, &value); err != nil {
				compilerError = err
				return
			}
			if err = compiler.AddResource("https://trove.local/contracts/"+name+".schema.json", value); err != nil {
				compilerError = err
				return
			}
		}
	})
	if compilerError != nil {
		return compilerError
	}
	compileMu.Lock()
	schema, err := compiler.Compile("https://trove.local/contracts/" + document + ".schema.json#/$defs/" + definition)
	compileMu.Unlock()
	if err != nil {
		return err
	}
	var value any
	if err = json.Unmarshal(raw, &value); err != nil {
		return err
	}
	return schema.Validate(value)
}

func DecodeBatch(raw []byte) (ServiceBatchRequest, error) {
	var request ServiceBatchRequest
	if len(raw) > 4*1024*1024 {
		return request, fmt.Errorf("RESOURCE_EXHAUSTED: request exceeds 4 MiB")
	}
	if err := Validate("service", "BatchRequest", raw); err != nil {
		return request, err
	}
	err := json.Unmarshal(raw, &request)
	return request, err
}

// WorkspaceServices are shell-independent. Context comes from the trusted adapter,
// never the renderer. A Wails facade maps its lifecycle to these context-aware calls.
type WorkspaceServices interface {
	AllocateIds(context.Context, ServiceExecutionContext, ServiceAllocateIdsRequest) ServiceAllocateIdsResponse
	PreviewImport(context.Context, ServiceExecutionContext, ServicePreviewImportRequest) ServicePreviewImportResponse
	GetImportReport(context.Context, ServiceExecutionContext, ServiceGetImportReportRequest) ServiceGetImportReportResponse
	ListMemory(context.Context, ServiceExecutionContext, ServiceListMemoryRequest) ServiceListMemoryResponse
	Batch(context.Context, ServiceExecutionContext, ServiceBatchRequest) ServiceBatchResponse
	ReadPage(context.Context, ServiceExecutionContext, ServiceReadPageRequest) ServiceReadPageResponse
	QueryRecords(context.Context, ServiceExecutionContext, ServiceQueryRecordsRequest) ServiceQueryRecordsResponse
	Search(context.Context, ServiceExecutionContext, ServiceSearchRequest) ServiceSearchResponse
	ResolveCitation(context.Context, ServiceExecutionContext, ServiceResolveCitationRequest) ServiceResolveCitationResponse
	ReadEvents(context.Context, ServiceExecutionContext, ServiceReadEventsRequest) ServiceReadEventsResponse
	Cancel(context.Context, ServiceExecutionContext, ServiceCancelRequest) ServiceCancelResponse
	PrepareFile(context.Context, ServiceExecutionContext, ServicePrepareFileRequest) ServicePrepareFileResponse
	EnqueueJob(context.Context, ServiceExecutionContext, JobsEnqueueJobRequest) JobsJobResponse
	GetJob(context.Context, ServiceExecutionContext, JobsGetJobRequest) JobsJobResponse
	CancelJob(context.Context, ServiceExecutionContext, JobsCancelJobRequest) JobsJobResponse
}
