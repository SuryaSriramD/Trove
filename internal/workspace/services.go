package workspace

import (
	"context"
	"encoding/json"
	"trove.local/phase0/contracts"
)

var _ contracts.WorkspaceServices = (*Kernel)(nil)

func (k *Kernel) AllocateIds(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.ServiceAllocateIdsRequest) contracts.ServiceAllocateIdsResponse {
	var out contracts.ServiceAllocateIdsResponse
	json.Unmarshal(k.Dispatch(ctx, e, "allocateIds", marshal(r)), &out)
	return out
}
func (k *Kernel) PreviewImport(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.ServicePreviewImportRequest) contracts.ServicePreviewImportResponse {
	var out contracts.ServicePreviewImportResponse
	json.Unmarshal(k.Dispatch(ctx, e, "previewImport", marshal(r)), &out)
	return out
}
func (k *Kernel) GetImportReport(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.ServiceGetImportReportRequest) contracts.ServiceGetImportReportResponse {
	var out contracts.ServiceGetImportReportResponse
	json.Unmarshal(k.Dispatch(ctx, e, "getImportReport", marshal(r)), &out)
	return out
}
func (k *Kernel) ListMemory(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.ServiceListMemoryRequest) contracts.ServiceListMemoryResponse {
	var out contracts.ServiceListMemoryResponse
	json.Unmarshal(k.Dispatch(ctx, e, "listMemory", marshal(r)), &out)
	return out
}
func (k *Kernel) ReadPage(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.ServiceReadPageRequest) contracts.ServiceReadPageResponse {
	var out contracts.ServiceReadPageResponse
	json.Unmarshal(k.Dispatch(ctx, e, "readPage", marshal(r)), &out)
	return out
}
func (k *Kernel) QueryRecords(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.ServiceQueryRecordsRequest) contracts.ServiceQueryRecordsResponse {
	var out contracts.ServiceQueryRecordsResponse
	json.Unmarshal(k.Dispatch(ctx, e, "queryRecords", marshal(r)), &out)
	return out
}
func (k *Kernel) Search(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.ServiceSearchRequest) contracts.ServiceSearchResponse {
	var out contracts.ServiceSearchResponse
	json.Unmarshal(k.Dispatch(ctx, e, "search", marshal(r)), &out)
	return out
}
func (k *Kernel) ResolveCitation(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.ServiceResolveCitationRequest) contracts.ServiceResolveCitationResponse {
	var out contracts.ServiceResolveCitationResponse
	json.Unmarshal(k.Dispatch(ctx, e, "resolveCitation", marshal(r)), &out)
	return out
}
func (k *Kernel) ReadEvents(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.ServiceReadEventsRequest) contracts.ServiceReadEventsResponse {
	var out contracts.ServiceReadEventsResponse
	json.Unmarshal(k.Dispatch(ctx, e, "readEvents", marshal(r)), &out)
	return out
}
func (k *Kernel) Cancel(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.ServiceCancelRequest) contracts.ServiceCancelResponse {
	var out contracts.ServiceCancelResponse
	json.Unmarshal(k.Dispatch(ctx, e, "cancel", marshal(r)), &out)
	return out
}
func (k *Kernel) PrepareFile(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.ServicePrepareFileRequest) contracts.ServicePrepareFileResponse {
	var out contracts.ServicePrepareFileResponse
	json.Unmarshal(k.Dispatch(ctx, e, "prepareFile", marshal(r)), &out)
	return out
}
func (k *Kernel) EnqueueJob(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.JobsEnqueueJobRequest) contracts.JobsJobResponse {
	var out contracts.JobsJobResponse
	json.Unmarshal(k.Dispatch(ctx, e, "enqueueJob", marshal(r)), &out)
	return out
}
func (k *Kernel) GetJob(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.JobsGetJobRequest) contracts.JobsJobResponse {
	var out contracts.JobsJobResponse
	json.Unmarshal(k.Dispatch(ctx, e, "getJob", marshal(r)), &out)
	return out
}
func (k *Kernel) CancelJob(ctx context.Context, e contracts.ServiceExecutionContext, r contracts.JobsCancelJobRequest) contracts.JobsJobResponse {
	var out contracts.JobsJobResponse
	json.Unmarshal(k.Dispatch(ctx, e, "cancelJob", marshal(r)), &out)
	return out
}
