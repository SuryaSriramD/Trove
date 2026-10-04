package workspace

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
)

func TestP2GraphTraceabilityScopeDeletionAndSemanticFallback(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a := createPage(t, k, e, "Orion research")
	b := createPage(t, k, e, "Orion notes")
	hidden := createPage(t, k, e, "Secret evidence")
	block := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": a, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "notes", "link": canonicalPageLink(b, "")}, map[string]any{"text": "hidden", "link": canonicalPageLink(hidden, "")}}}}}}, "page", a, "block", block))
	r, err := knowledgeGraph(ctx, k, e, map[string]any{"pageId": a, "includeSemantic": true, "query": "Orion"})
	if err != nil {
		t.Fatal(err)
	}
	g := r.(KnowledgeGraph)
	if len(g.Edges) < 2 || g.Warning == "" {
		t.Fatal(g)
	}
	explicit := 0
	for _, edge := range g.Edges {
		if edge.Kind == "explicit" {
			explicit++
			if edge.Location.BlockId == nil || string(*edge.Location.BlockId) != block {
				t.Fatal(edge)
			}
			resolved := testOK(t, testDispatch(t, k, e, "resolveCitation", recoveryRequest(k, map[string]any{"location": edge.Location})))
			if resolved["state"] != "current" {
				t.Fatal(resolved)
			}
		} else if edge.Kind != "keyword" {
			t.Fatal("unavailable semantic engine misrepresented", edge)
		}
	}
	if explicit != 2 {
		t.Fatal(g)
	}
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(a), contracts.DomainId(b)}}})
	r, err = knowledgeGraph(ctx, k, scoped, map[string]any{"pageId": a})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range r.(KnowledgeGraph).Nodes {
		if n.ID == hidden {
			t.Fatal("graph exposed denied target")
		}
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.trash", "pageId": b, "includeDescendants": true}}, "page", b))
	r, err = knowledgeGraph(ctx, k, e, map[string]any{"pageId": a})
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range r.(KnowledgeGraph).Edges {
		if edge.Target == b {
			t.Fatal("deleted relationship remained navigable")
		}
	}
}
func p2ReviewFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, policy string) (string, string, string) {
	page := createPage(t, k, e, "Review inbox")
	source := createPage(t, k, e, "Current research evidence")
	block := NewID()
	plan := PersonalReviewPlan{Type: "trove.personal-review/v1", Name: "Weekly review", Enabled: true, SourcePageIDs: []string{source}, Schedule: AutomationTrigger{Type: "schedule", Timezone: "America/New_York", Frequency: "daily", Hour: 9, MissedPolicy: policy, CatchUpLimit: 1}}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": string(marshal(plan)), "language": "trove-review"}}}}, "page", page, "block", block))
	return page, source, block
}
func TestP2ReviewActivationSchedulingDedupRestartAndApproval(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page, source, block := p2ReviewFixture(t, k, e, "catch_up")
	at := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	if err := k.RunPersonalReviewTick(ctx, e, at); err != nil {
		t.Fatal(err)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM personal_review_runs").Scan(&count)
	if count != 0 {
		t.Fatal("imported or pasted plan ran before activation")
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "review.activate", "pageId": page, "planBlockId": block, "enabled": true}}, "page", page))
	k.db.Exec("UPDATE personal_review_schedule SET next_at=? WHERE block_id=?", at.Add(-72*time.Hour).Format(time.RFC3339Nano), block)
	for i := 0; i < 2; i++ {
		if err := k.RunPersonalReviewTick(ctx, e, at); err != nil {
			t.Fatal(err)
		}
	}
	k.db.QueryRow("SELECT count(*) FROM personal_review_runs").Scan(&count)
	if count != 1 {
		t.Fatal("catchup did not remain bounded/deduplicated", count)
	}
	var runID string
	k.db.QueryRow("SELECT id FROM personal_review_runs").Scan(&runID)
	var text string
	k.db.QueryRow("SELECT json_extract(payload_json,'$.text') FROM personal_review_runs").Scan(&text)
	if !strings.Contains(text, "Current research evidence") {
		t.Fatal(text)
	}
	var blocks int
	k.db.QueryRow("SELECT count(*) FROM blocks WHERE page_id=?", page).Scan(&blocks)
	if blocks != 1 {
		t.Fatal("scheduler committed unreviewed content")
	}
	path := k.Path()
	k.Close()
	var err error
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	if err = k.RunPersonalReviewTick(ctx, e, at); err != nil {
		t.Fatal(err)
	}
	k.db.QueryRow("SELECT count(*) FROM personal_review_runs").Scan(&count)
	if count != 1 {
		t.Fatal("restart repeated occurrence")
	}
	accepted := NewID()
	command := map[string]any{"kind": "review.accept", "pageId": page, "runId": runID, "blockId": accepted, "approved": true}
	testOK(t, dbBatch(t, k, e, []any{command}, "page", page, "page", source, "block", accepted))
	k.db.QueryRow("SELECT count(*) FROM blocks WHERE page_id=?", page).Scan(&blocks)
	if blocks != 2 {
		t.Fatal("approved digest missing")
	}
	testCode(t, dbBatch(t, k, e, []any{command}, "page", page, "page", source, "block", accepted), "CONFLICT")
}
func TestP2ReviewRejectsStaleAndChangedPlansAndLogsSkip(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page, source, block := p2ReviewFixture(t, k, e, "skip")
	plan, err := readReviewPlan(ctx, k.db, block)
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.preparePersonalReview(ctx, e, plan, "manual:test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": source, "title": "Changed evidence"}}, "page", source))
	id := NewID()
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "review.accept", "pageId": page, "runId": run.ID, "blockId": id, "approved": true}}, "page", page, "page", source, "block", id), "CONFLICT")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "review.activate", "pageId": page, "planBlockId": block, "enabled": true}}, "page", page))
	at := time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC)
	k.db.Exec("UPDATE personal_review_schedule SET next_at=? WHERE block_id=?", at.Add(-48*time.Hour).Format(time.RFC3339Nano), block)
	if err = k.RunPersonalReviewTick(ctx, e, at); err != nil {
		t.Fatal(err)
	}
	var n int
	k.db.QueryRow("SELECT count(*) FROM personal_review_runs WHERE status='skipped'").Scan(&n)
	if n != 1 {
		t.Fatal("missed skip receipt missing")
	}
	plan.Plan.Name = "Edited plan"
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "code", "schemaVersion": 1, "data": map[string]any{"language": "trove-review", "source": string(marshal(plan.Plan))}}}, "page", page, "block", block))
	current, err := readReviewPlan(ctx, k.db, block)
	if err != nil {
		t.Fatal(err)
	}
	if current.Activated {
		t.Fatal("edited source inherited activation")
	}
}
func TestP2OCRQualityScopeSearchAndResourceBounds(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Image evidence")
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 100, 100))); err != nil {
		t.Fatal(err)
	}
	prepared := stageTestFile(t, k, e, data.String())
	file, _ := attachTestFile(t, k, e, page, prepared, true)
	k.SetOCRProvider(func(ctx context.Context, f *os.File) (OCRResult, error) {
		return OCRResult{Lines: []OCRLine{{Text: "Nebulax readable evidence", Confidence: 0.97}, {Text: "Uncertain secret", Confidence: 0.2}}}, nil
	})
	defer k.SetOCRProvider(nil)
	out := testOK(t, testDispatch(t, k, e, "ocr.run", recoveryRequest(k, map[string]any{"fileId": file})))
	if out["acceptedLines"] != float64(1) || out["rejectedLines"] != float64(1) || strings.Contains(str(out["text"]), "Uncertain") {
		t.Fatal(out)
	}
	if len(querySearch(t, k, e, "Nebulax", nil)["hits"].([]any)) != 1 {
		t.Fatal("OCR projection not searchable")
	}
	other := createPage(t, k, e, "Unrelated")
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(other)}}})
	testCode(t, testDispatch(t, k, scoped, "ocr.run", recoveryRequest(k, map[string]any{"fileId": file})), "SCOPE_DENIED")
	k.SetOCRProvider(func(ctx context.Context, f *os.File) (OCRResult, error) {
		return OCRResult{Lines: []OCRLine{{Text: "Invalid", Confidence: math.NaN()}}}, nil
	})
	testCode(t, testDispatch(t, k, e, "ocr.run", recoveryRequest(k, map[string]any{"fileId": file})), "WORKER_FAILED")
	k.SetOCRProvider(nil)
	testCode(t, testDispatch(t, k, e, "ocr.run", recoveryRequest(k, map[string]any{"fileId": file})), "WORKER_FAILED")
	plain := stageTestFile(t, k, e, "not an image")
	plainID, _ := attachTestFile(t, k, e, page, plain, false)
	testCode(t, testDispatch(t, k, e, "ocr.run", recoveryRequest(k, map[string]any{"fileId": plainID})), "INVALID_REQUEST")
	oversized := append([]byte(nil), data.Bytes()...)
	binary.BigEndian.PutUint32(oversized[16:20], 200001)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	large := stageTestFile(t, k, e, string(oversized))
	largeID, _ := attachTestFile(t, k, e, page, large, false)
	k.SetOCRProvider(func(context.Context, *os.File) (OCRResult, error) {
		t.Fatal("oversized raster reached worker")
		return OCRResult{}, nil
	})
	testCode(t, testDispatch(t, k, e, "ocr.run", recoveryRequest(k, map[string]any{"fileId": largeID})), "RESOURCE_EXHAUSTED")
}

func TestP2ReviewApprovedMemoryRemainsEvidenceAndRejectsForgetting(t *testing.T) {
	k, e := testKernel(t)
	page, source, block := p2ReviewFixture(t, k, e, "skip")
	memory, memoryPage := memoryFixture(t, k, e, source)
	p, err := readReviewPlan(context.Background(), k.db, block)
	if err != nil {
		t.Fatal(err)
	}
	p.Plan.IncludeMemory = true
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "code", "schemaVersion": 1, "data": map[string]any{"language": "trove-review", "source": string(marshal(p.Plan))}}}, "page", page, "block", block))
	p, err = readReviewPlan(context.Background(), k.db, block)
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.preparePersonalReview(context.Background(), e, p, "manual:memory", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.Text, "Approved memory: Use concise prose") {
		t.Fatal("approved relevant memory missing", run.Text)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.forget", "memoryId": memory}}, "page", memoryPage, "memory", memory))
	id := NewID()
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "review.accept", "pageId": page, "runId": run.ID, "blockId": id, "approved": true}}, "page", page, "page", source, "page", memoryPage, "block", id), "CONFLICT")
}

func TestP2ReviewFailedSourceDoesNotStarveAnotherPlan(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	badPage, badSource, badBlock := p2ReviewFixture(t, k, e, "catch_up")
	goodPage, _, goodBlock := p2ReviewFixture(t, k, e, "catch_up")
	for _, p := range [][2]string{{badPage, badBlock}, {goodPage, goodBlock}} {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "review.activate", "pageId": p[0], "planBlockId": p[1], "enabled": true}}, "page", p[0]))
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.trash", "pageId": badSource, "includeDescendants": true}}, "page", badSource))
	at := time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC)
	if _, err := k.db.Exec("UPDATE personal_review_schedule SET next_at=?", at.Add(-48*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := k.RunPersonalReviewTick(ctx, e, at); err == nil {
		t.Fatal("missing failed-plan diagnostic")
	}
	var failed, pending int
	k.db.QueryRow("SELECT count(*) FROM personal_review_runs WHERE status='failed'").Scan(&failed)
	k.db.QueryRow("SELECT count(*) FROM personal_review_runs WHERE status='pending' AND block_id=?", goodBlock).Scan(&pending)
	if failed != 1 || pending != 1 {
		t.Fatalf("failure starved valid review: failed=%d pending=%d", failed, pending)
	}
	if err := k.RunPersonalReviewTick(ctx, e, at); err != nil {
		t.Fatal("failed occurrence did not advance", err)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM personal_review_runs").Scan(&count)
	if count != 2 {
		t.Fatal("duplicate receipts", count)
	}
}

func TestP2ReviewMemoryIdentityFencesInterleavedPageRevision(t *testing.T) {
	k, e := testKernel(t)
	page, source, block := p2ReviewFixture(t, k, e, "skip")
	memory, memoryPage := memoryFixture(t, k, e, source)
	p, err := readReviewPlan(context.Background(), k.db, block)
	if err != nil {
		t.Fatal(err)
	}
	p.Plan.IncludeMemory = true
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "code", "schemaVersion": 1, "data": map[string]any{"language": "trove-review", "source": string(marshal(p.Plan))}}}, "page", page, "block", block))
	p, err = readReviewPlan(context.Background(), k.db, block)
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.preparePersonalReview(context.Background(), e, p, "manual:race", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Memories) != 1 {
		t.Fatal("memory identity/revision missing")
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.forget", "memoryId": memory}}, "page", memoryPage, "memory", memory))
	// Reproduce the original interleaving: retrieval captured approved content,
	// but read its owning page revision after another writer forgot the memory.
	var current string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", memoryPage).Scan(&current)
	for i := range run.Sources {
		if string(run.Sources[i].PageId) == memoryPage {
			run.Sources[i].SourceRevision = contracts.DomainRevision(current)
		}
	}
	raw, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.db.Exec("UPDATE personal_review_runs SET payload_json=? WHERE id=?", string(raw), run.ID); err != nil {
		t.Fatal(err)
	}
	id := NewID()
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "review.accept", "pageId": page, "runId": run.ID, "blockId": id, "approved": true}}, "page", page, "page", source, "page", memoryPage, "block", id), "CONFLICT")
}
