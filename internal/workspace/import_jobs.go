package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"trove.local/phase0/contracts"
)

func enqueuePortableImport(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	p, err := loadImportPlan(ctx, k.db, str(r["planId"]))
	if err != nil {
		return nil, err
	}
	if p.Hash != str(r["planHash"]) {
		return nil, Fail("CONFLICT", "Import preview changed")
	}
	for _, item := range p.Items {
		if item.Conflict != "" && item.Choice == "" {
			return nil, Fail("CONFLICT", "Resolve re-import conflict: "+item.Locator)
		}
	}
	result, err := k.enqueueJob(ctx, e, map[string]any{"kind": "IMPORT_NOTION", "idempotencyKey": p.ID + "-import", "payload": map[string]any{"kind": "import", "stagingId": p.ID, "sourceRevision": p.Hash}})
	if err != nil {
		return nil, err
	}
	job := result.(contracts.JobsJob)
	err = k.write(ctx, func(tx *sql.Tx) error {
		latest, err := loadImportPlan(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		latest.JobID = string(job.JobId)
		return saveImportPlan(ctx, tx, latest)
	})
	return result, err
}
func runPortableImportJob(ctx context.Context, k *Kernel, j contracts.JobsJob) error {
	payload, ok := j.Payload.Value.(contracts.JobsJobPayloadVariant1)
	if !ok || j.LeaseToken == nil {
		return Fail("INVALID_REQUEST", "Expected leased import job")
	}
	e, err := k.JobExecution(ctx, string(j.JobId))
	if err != nil {
		return err
	}
	defer k.RevokeGrant(string(e.ScopeGrantId))
	sequence := int64(1)
	if j.Progress != nil {
		sequence = j.Progress.Sequence + 1
	}
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		p, err := loadImportPlan(ctx, k.db, string(payload.StagingId))
		if err != nil {
			return err
		}
		if p.Hash != payload.SourceRevision {
			return Fail("CONFLICT", "Import plan changed after job approval")
		}
		if p.State == "complete" {
			return nil
		}
		unit := fmt.Sprintf("%t-%d", p.Initialized, p.Cursor)
		raw := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.id, "mutation": map[string]any{"scopeGrantId": e.ScopeGrantId, "idempotencyKey": string(j.JobId) + "-" + unit, "expectedRevisions": []any{}}, "commands": []any{map[string]any{"kind": "import.applyPlan", "planId": p.ID, "planHash": p.Hash, "resolutions": []any{}}}}
		var request contracts.ServiceBatchRequest
		if err = json.Unmarshal(marshal(raw), &request); err != nil {
			return err
		}
		response := k.CommitJobBatch(ctx, e, string(j.JobId), string(*j.LeaseToken), unit, request)
		if failed, ok := response.Value.(contracts.ServiceBatchResponseVariant2); ok {
			return Fail(failed.Error.Code, failed.Error.Message)
		}
		if _, ok := response.Value.(contracts.ServiceBatchResponseVariant1); !ok {
			return Fail("INTEGRITY_ERROR", "Import job returned no commit receipt")
		}
		latest, err := loadImportPlan(ctx, k.db, p.ID)
		if err != nil {
			return err
		}
		total := int64(len(latest.Items))
		message := "Importing " + latest.SourceName
		if err = k.ProgressJob(ctx, string(j.JobId), string(*j.LeaseToken), contracts.JobsProgress{Sequence: sequence, Completed: int64(latest.Cursor), Total: &total, Unit: "items", Message: &message}); err != nil {
			return err
		}
		sequence++
	}
}

func previewCanonicalImport(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	value, err := previewPortableImport(ctx, k, e, r)
	if err != nil {
		return nil, err
	}
	p := value.(ImportPlan)
	plans := []any{}
	guards := []any{}
	for _, item := range p.Items {
		if item.MappingID == "" {
			continue
		}
		resolution := "no_change"
		outcome := "unchanged"
		if item.Conflict != "" {
			resolution = "needs_review"
			outcome = "conflict"
		}
		plans = append(plans, map[string]any{"id": item.MappingID, "mappingId": item.MappingID, "expectedLocalRevision": item.ExpectedRevision, "incomingSourceRevision": item.Hash, "identityStatus": "matched", "sourceCompleteness": p.SourceCompleteness, "fields": []any{map[string]any{"path": "/body", "outcome": outcome, "base": map[string]any{"present": false}, "local": map[string]any{"present": true, "value": map[string]any{"pageId": item.PageID, "revision": item.ExpectedRevision}}, "incoming": map[string]any{"present": true, "value": map[string]any{"rawFileId": item.RawFileID, "blocks": item.Blocks}}}}, "sourceDeleted": false, "localDeleted": false, "resolution": resolution})
		guards = append(guards, required("page", item.PageID, item.ExpectedRevision))
	}
	return map[string]any{"planId": p.ID, "planHash": p.Hash, "report": portableImportReport(p), "reimportPlans": plans, "requiredRevisions": guards}, nil
}
