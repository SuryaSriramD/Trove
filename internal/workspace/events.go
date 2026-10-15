package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"trove.local/phase0/contracts"
)

func (k *Kernel) Subscribe() (<-chan json.RawMessage, func()) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.listenerSeq++
	id := k.listenerSeq
	ch := make(chan json.RawMessage, 128)
	if k.closed {
		close(ch)
	} else {
		k.listeners[id] = ch
	}
	return ch, func() {
		k.mu.Lock()
		defer k.mu.Unlock()
		if _, ok := k.listeners[id]; ok {
			delete(k.listeners, id)
			close(ch)
		}
	}
}
func (k *Kernel) SubscribeChanges() (<-chan json.RawMessage, func()) { return k.Subscribe() }
func (k *Kernel) publish(event json.RawMessage) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for id, ch := range k.listeners {
		select {
		case ch <- event:
		default: // A slow consumer must catch up from the durable cursor; never silently skip a commit.
			close(ch)
			delete(k.listeners, id)
		}
	}
}
func (k *Kernel) Dispatch(ctx context.Context, e contracts.ServiceExecutionContext, method string, raw json.RawMessage) json.RawMessage {
	started := time.Now()
	outcome, code := "completed", "none"
	fail := func(id string, err error) json.RawMessage {
		outcome, code = diagnosticOutcome(err)
		return errorResponse(id, err)
	}
	defer func() {
		k.diagnostics.Record(diagnosticComponent(method), "request", outcome, code, time.Since(started), 0)
	}()
	if len(raw) > 4*1024*1024 {
		return fail("", Fail("RESOURCE_EXHAUSTED", "Request exceeds 4 MiB"))
	}
	var r map[string]any
	if err := json.Unmarshal(raw, &r); err != nil {
		return fail("", Fail("INVALID_REQUEST", "Invalid JSON"))
	}
	id := str(r["requestId"])
	if method == "batch" {
		response := k.batch(ctx, e, raw)
		outcome, code = diagnosticBatchResponse(response)
		return response
	}
	if err := k.checkEnvelope(e, r); err != nil {
		return fail(id, err)
	}
	defs := map[string]string{"scheduling.preview": "SchedulingPreviewRequest", "recordLayouts.get": "RecordLayoutRequest", "skills.list": "SkillsListRequest", "skills.preview": "SkillPreviewRequest", "imports.configure": "ConfigureImportRequest", "meetings.prepareExtraction": "MeetingExtractionPrepareRequest", "meetings.extractions": "MeetingExtractionListRequest", "meetings.createFromTranscript": "MeetingExtractionCreateRequest", "operations.list": "OperationsListRequest", "operations.previewUndo": "OperationPreviewUndoRequest", "page.freshness": "PageFreshnessRequest", "page.reviewQueue": "ReviewQueueRequest", "image.start": "ImageDescriptionStartRequest", "image.list": "ImageDescriptionListRequest", "image.cancel": "ImageDescriptionControlRequest", "review.propose": "PersonalReviewControlRequest", "review.apply": "PersonalReviewControlRequest", "review.retry": "PersonalReviewControlRequest", "review.cancel": "PersonalReviewControlRequest", "graph.query": "GraphQueryRequest", "ocr.status": "OCRStatusRequest", "ocr.run": "OCRRunRequest", "review.list": "ReviewListRequest", "review.run": "ReviewRunRequest", "databaseP2": "DatabaseP2Request", "databaseChart": "DatabaseChartRequest", "pageDisplay.get": "PageDisplayGetRequest", "history.list": "HistoryListRequest", "history.previewRestore": "HistoryPreviewRestoreRequest", "history.trash": "HistoryTrashRequest", "history.recoveryPoint": "HistoryRecoveryPointRequest", "workspace.snapshot": "WorkspaceSnapshotRequest", "links.backlinks": "BacklinksRequest", "jobs.list": "JobsListRequest", "allocateIds": "AllocateIdsRequest", "readPage": "ReadPageRequest", "readEvents": "ReadEventsRequest", "cancel": "CancelRequest", "queryRecords": "QueryRecordsRequest", "search": "SearchRequest", "resolveCitation": "ResolveCitationRequest", "prepareFile": "PrepareFileRequest", "previewImport": "PreviewImportRequest", "getImportReport": "GetImportReportRequest", "listMemory": "ListMemoryRequest"}
	if def := defs[method]; def != "" {
		if err := contracts.Validate("service", def, raw); err != nil {
			return fail(id, Fail("INVALID_REQUEST", err.Error()))
		}
	}
	if method == "enqueueJob" || method == "getJob" || method == "cancelJob" {
		def := strings.ToUpper(method[:1]) + method[1:] + "Request"
		if err := contracts.Validate("jobs", def, raw); err != nil {
			return fail(id, Fail("INVALID_REQUEST", err.Error()))
		}
	}
	var result any
	var err error
	switch method {
	case "allocateIds":
		ids := []string{}
		for i := int64(0); i < number(r["count"]); i++ {
			ids = append(ids, NewID())
		}
		result = map[string]any{"ids": ids}
	case "readPage":
		if err = k.authorizePage(ctx, k.db, e, str(r["pageId"])); err == nil {
			var tx *sql.Tx
			tx, err = k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
			if err == nil {
				defer tx.Rollback()
				if revision := str(r["atRevision"]); revision != "" {
					var raw string
					err = tx.QueryRowContext(ctx, "SELECT snapshot_json FROM page_snapshots WHERE page_id=? AND revision=?", r["pageId"], revision).Scan(&raw)
					if err == nil {
						var p contracts.ServiceReadPageResult
						err = json.Unmarshal([]byte(raw), &p)
						result = p
					}
				} else {
					result, err = readPageTx(ctx, tx, str(r["pageId"]))
				}
			}
		}
	case "readEvents":
		result, err = k.readEvents(ctx, e, str(r["afterCursor"]), number(r["limit"]))
	case "cancel":
		result, err = k.cancelMutation(ctx, e, str(r["cancellationId"]))
	case "workspace.snapshot":
		if str(r["contentMode"]) == "shell" {
			result, err = k.shellSnapshot(ctx, e)
		} else {
			result, err = k.snapshot(ctx, e)
		}
	case "jobs.list":
		result, err = k.listJobs(ctx, e, r)
	case "enqueueJob":
		result, err = k.enqueueJob(ctx, e, r)
	case "getJob":
		result, err = k.getAuthorizedJob(ctx, e, str(r["jobId"]))
	case "cancelJob":
		result, err = k.cancelJob(ctx, e, str(r["jobId"]), str(r["expectedRevision"]))
	case "links.backlinks":
		page := str(r["pageId"])
		if err = k.authorizePage(ctx, k.db, e, page); err == nil {
			rows, er := k.db.QueryContext(ctx, "SELECT source_page_id,source_block_id FROM page_links WHERE target_page_id=?", page)
			err = er
			if err == nil {
				items := []any{}
				for rows.Next() {
					var p, b string
					rows.Scan(&p, &b)
					if k.authorizePage(ctx, k.db, e, p) == nil {
						items = append(items, map[string]any{"pageId": p, "blockId": b})
					}
				}
				err = rows.Err()
				rows.Close()
				result = map[string]any{"links": items}
			}
		}
	case "resolveCitation":
		result, err = k.resolveCitation(ctx, e, r)
	default:
		if h := queryHandlers[method]; h != nil {
			result, err = h(ctx, k, e, r)
		} else {
			err = Fail("UNSUPPORTED_SCHEMA", "Service is not available: "+method)
		}
	}
	if err != nil {
		return fail(id, err)
	}
	return success(result)
}
func (k *Kernel) readEvents(ctx context.Context, e contracts.ServiceExecutionContext, after string, limit int64) (any, error) {
	rows, err := k.db.QueryContext(ctx, "SELECT cursor,payload_json FROM committed_events WHERE cursor>? ORDER BY cursor LIMIT ?", after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []json.RawMessage{}
	next := after
	for rows.Next() {
		var cursor, raw string
		if err = rows.Scan(&cursor, &raw); err != nil {
			return nil, err
		}
		next = cursor
		var event contracts.ServiceCommittedChangeEvent
		if err = json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		visible := []contracts.ServiceRevisionChange{}
		for _, change := range event.Revisions {
			if change.Deleted && change.Target.EntityType == "block" {
				if scope, ok := e.Scope.Value.(contracts.DomainScopeVariant1); ok && string(scope.WorkspaceId) == k.id {
					visible = append(visible, change)
					continue
				}
				var before string
				if err = k.db.QueryRowContext(ctx, "SELECT before_json FROM mutation_history WHERE transaction_id=?", event.TransactionId).Scan(&before); err != nil {
					return nil, err
				}
				snapshots := map[string]contracts.ServiceReadPageResult{}
				if err = json.Unmarshal([]byte(before), &snapshots); err != nil {
					return nil, err
				}
				for page, snapshot := range snapshots {
					if k.authorizePage(ctx, k.db, e, page) != nil {
						continue
					}
					for _, block := range snapshot.Blocks {
						if block.Id == change.Target.EntityId {
							visible = append(visible, change)
						}
					}
				}
				continue
			}
			if k.authorizeEntity(ctx, k.db, e, change.Target.EntityType, string(change.Target.EntityId)) != nil {
				continue
			}
			visible = append(visible, change)
		}
		if len(visible) > 0 {
			event.Revisions = visible
			events = append(events, marshal(event))
		}
	}
	return map[string]any{"events": events, "nextCursor": next, "requiresSnapshot": false}, rows.Err()
}
func (k *Kernel) snapshot(ctx context.Context, e contracts.ServiceExecutionContext) (any, error) {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := map[string]any{"contractVersion": "1.0.0", "workspaceId": k.id, "scopeGrantId": e.ScopeGrantId, "snapshotCursor": "0"}
	for _, name := range []string{"pages", "blocks", "collections", "properties", "records", "views", "containers", "containerCollections", "taskFacets", "files", "meetings", "memories", "projectScopes", "transcriptSegments", "importMappings"} {
		out[name] = []any{}
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM pages ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	pages := []contracts.DomainPage{}
	blocks := []contracts.DomainBlock{}
	for _, id := range ids {
		if k.authorizePage(ctx, tx, e, id) != nil {
			continue
		}
		p, err := readPageTx(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		pages = append(pages, p.Page)
		blocks = append(blocks, p.Blocks...)
		out["snapshotCursor"] = p.SnapshotCursor
	}
	out["pages"] = pages
	out["blocks"] = blocks
	for _, name := range []string{"collections", "properties", "records", "views", "containers", "containerCollections", "taskFacets", "files", "meetings", "memories", "projectScopes", "transcriptSegments", "importMappings"} {
		if h := snapshotReaders[name]; h != nil {
			v, err := h(ctx, k, tx, e)
			if err != nil {
				return nil, err
			}
			out[name] = v
		}
	}
	return out, nil
}

type SnapshotReader func(context.Context, *Kernel, *sql.Tx, contracts.ServiceExecutionContext) (any, error)

var snapshotReaders = map[string]SnapshotReader{}

func RegisterSnapshotReader(name string, h SnapshotReader) { snapshotReaders[name] = h }
func (k *Kernel) resolveCitation(ctx context.Context, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	loc, ok := r["location"].(map[string]any)
	if !ok {
		return nil, Fail("INVALID_REQUEST", "Missing location")
	}
	if err := contracts.Validate("domain", "Location", marshal(loc)); err != nil {
		return nil, Fail("INVALID_REQUEST", "Invalid citation location")
	}
	page := str(loc["pageId"])
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = k.authorizePage(ctx, tx, e, page); err != nil {
		return nil, err
	}
	var revision, title string
	var deleted sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT revision,title,deleted_at FROM pages WHERE id=?", page).Scan(&revision, &title, &deleted)
	out := map[string]any{"location": loc, "state": "current"}
	if errors.Is(err, sql.ErrNoRows) {
		out["state"] = "unavailable"
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	out["currentRevision"] = revision
	if deleted.Valid {
		out["state"] = "deleted"
		return out, nil
	}
	var hidden int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM memory_entries WHERE page_id=? AND status<>'active'", page).Scan(&hidden); err != nil {
		return nil, err
	}
	if hidden > 0 {
		out["state"] = "unavailable"
		return out, nil
	}
	freshness, err := pageFreshness(ctx, tx, page, time.Now())
	if err != nil {
		return nil, err
	}
	out["freshness"] = freshness
	if revision != str(loc["sourceRevision"]) {
		out["state"] = "stale"
		return out, nil
	}
	if transcript, ok := loc["transcript"].(map[string]any); ok {
		var start, end int64
		var meeting, block, sourcePage string
		err = tx.QueryRowContext(ctx, "SELECT s.meeting_id,s.block_id,s.start_ms,s.end_ms,m.page_id FROM transcript_segments s JOIN meetings m ON m.id=s.meeting_id WHERE s.id=?", transcript["segmentId"]).Scan(&meeting, &block, &start, &end, &sourcePage)
		if errors.Is(err, sql.ErrNoRows) {
			out["state"] = "unavailable"
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if meeting != str(transcript["meetingId"]) || block != str(loc["blockId"]) || sourcePage != page || number(transcript["startMs"]) < start || number(transcript["endMs"]) > end || number(transcript["endMs"]) < number(transcript["startMs"]) {
			out["state"] = "unavailable"
			return out, nil
		}
	}
	out["excerpt"] = title
	if block := str(loc["blockId"]); block != "" {
		var raw string
		err = tx.QueryRowContext(ctx, "SELECT data_json FROM blocks WHERE id=? AND page_id=?", block, page).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			out["state"] = "unavailable"
			delete(out, "excerpt")
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		var data any
		if err = json.Unmarshal([]byte(raw), &data); err != nil {
			return nil, err
		}
		out["excerpt"] = searchReadable(data)
	}
	return out, nil
}
