package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

const notionDBA = "11111111111111111111111111111111"
const notionDBB = "22222222222222222222222222222222"
const notionSourceA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const notionSourceB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const notionRecordA = "cccccccccccccccccccccccccccccccc"
const notionRecordB = "dddddddddddddddddddddddddddddddd"
const notionViewA = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
const notionTemplateA = "99999999999999999999999999999999"
const notionViewB = "ffffffffffffffffffffffffffffffff"

type notionDatabaseFixture struct {
	denyProperty  bool
	propertyCalls int
	refreshCalls  int
	fileDenied    bool
	changed       bool
}

func (f *notionDatabaseFixture) client(t *testing.T) *http.Client {
	return &http.Client{Transport: notionFixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.notion.com" {
			if r.Header.Get("Authorization") != "" {
				t.Fatal("token leaked to storage")
			}
			if !f.fileDenied {
				f.fileDenied = true
				return notionResponse(403, `expired`), nil
			}
			if r.URL.Path == "/license" {
				return notionResponse(200, "offline license"), nil
			}
			return notionResponse(200, "offline invoice"), nil
		}
		if r.Header.Get("Notion-Version") != notionCaptureVersion {
			t.Fatal("capture version missing")
		}
		path := r.URL.Path
		if path == "/v1/databases/"+notionDBA {
			return notionResponse(200, fmt.Sprintf(`{"object":"database","id":%q,"title":[{"plain_text":"Primary container"}],"data_sources":[{"id":%q,"name":"Projects"},{"id":%q,"name":"People"}]}`, notionDBA, notionSourceA, notionSourceB)), nil
		}
		if path == "/v1/databases/"+notionDBB {
			return notionResponse(200, fmt.Sprintf(`{"object":"database","id":%q,"title":[{"plain_text":"Linked container"}],"data_sources":[]}`, notionDBB)), nil
		}
		if path == "/v1/data_sources/"+notionSourceA {
			return notionResponse(200, fmt.Sprintf(`{"object":"data_source","id":%q,"title":[{"plain_text":"Projects"}],"parent":{"database_id":%q},"properties":{"Name":{"id":"title","type":"title","title":{}},"Price":{"id":"price","type":"number","number":{"format":"dollar"}},"Notes":{"id":"r%%3Ai","type":"rich_text","rich_text":{}},"Owner":{"id":"owner","type":"relation","relation":{"data_source_id":%q,"type":"single_property"}},"Total":{"id":"calc","type":"formula","formula":{"expression":"prop(\"Price\") * 2"}},"Attachments":{"id":"files","type":"files","files":{}}}}`, notionSourceA, notionDBA, notionSourceB)), nil
		}
		if path == "/v1/data_sources/"+notionSourceB {
			return notionResponse(200, fmt.Sprintf(`{"object":"data_source","id":%q,"title":[{"plain_text":"People"}],"parent":{"database_id":%q},"properties":{"Name":{"id":"title","type":"title","title":{}}}}`, notionSourceB, notionDBA)), nil
		}
		if strings.HasSuffix(path, "/templates") {
			if strings.Contains(path, notionSourceA) {
				return notionResponse(200, fmt.Sprintf(`{"templates":[{"id":%q,"name":"Planning template","is_default":true}],"has_more":false,"next_cursor":null}`, notionTemplateA)), nil
			}
			return notionResponse(200, `{"templates":[],"has_more":false,"next_cursor":null}`), nil
		}
		if strings.HasSuffix(path, "/query") {
			if r.Method != "POST" {
				t.Fatal("query must POST")
			}
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"page_size":100`) {
				t.Fatal(string(body))
			}
			id := notionRecordA
			if strings.Contains(path, notionSourceB) {
				id = notionRecordB
			} else if !strings.Contains(string(body), "query-next") {
				return notionResponse(200, fmt.Sprintf(`{"object":"list","results":[{"object":"page","id":%q}],"has_more":true,"next_cursor":"query-next"}`, id)), nil
			}
			return notionResponse(200, fmt.Sprintf(`{"object":"list","results":[{"object":"page","id":%q}],"has_more":false,"next_cursor":null}`, id)), nil
		}
		if path == "/v1/views" {
			id := notionViewA
			if r.URL.Query().Get("database_id") == notionDBB {
				id = notionViewB
			}
			return notionResponse(200, fmt.Sprintf(`{"object":"list","results":[{"object":"view","id":%q}],"has_more":false,"next_cursor":null}`, id)), nil
		}
		if strings.HasPrefix(path, "/v1/views/") {
			id := strings.TrimPrefix(path, "/v1/views/")
			parent := notionDBA
			if id == notionViewB {
				parent = notionDBB
			}
			return notionResponse(200, fmt.Sprintf(`{"object":"view","id":%q,"parent":{"type":"database_id","database_id":%q},"data_source_id":%q,"name":"Shared projects","type":"table","filter":{"property":"Price","number":{"greater_than":10}},"sorts":[{"property":"Price","direction":"descending"}],"configuration":{"type":"table","properties":[{"property_id":"title","visible":true,"width":300},{"property_id":"price","visible":true,"width":220}]}}`, id, parent, notionSourceA)), nil
		}
		if strings.HasSuffix(path, "/children") {
			if strings.Contains(path, notionTemplateA) {
				return notionResponse(200, `{"object":"list","results":[{"id":"88888888888888888888888888888888","type":"paragraph","paragraph":{"rich_text":[{"plain_text":"Template body offline","annotations":{"bold":true}}]}}],"has_more":false,"next_cursor":null}`), nil
			}
			return notionResponse(200, `{"object":"list","results":[],"has_more":false,"next_cursor":null}`), nil
		}
		if strings.Contains(path, "/properties/") {
			prop := strings.Split(path, "/properties/")[1]
			page := strings.Split(strings.TrimPrefix(path, "/v1/pages/"), "/")[0]
			if prop == "title" {
				text := "Migration"
				if page == notionRecordB {
					text = "Asha"
				}
				if page == notionTemplateA {
					text = "Planning template"
				}
				return notionResponse(200, fmt.Sprintf(`{"object":"list","results":[{"object":"property_item","id":"title","type":"title","title":{"plain_text":%q}}],"has_more":false,"property_item":{"id":"title","type":"title"}}`, text)), nil
			}
			if prop == "r:i" {
				f.propertyCalls++
				if f.denyProperty {
					response := notionResponse(429, `{"code":"rate_limited"}`)
					response.Header.Set("Retry-After", "9")
					return response, nil
				}
				if r.URL.Query().Get("start_cursor") == "notes-next" {
					return notionResponse(200, `{"object":"list","results":[{"object":"property_item","id":"r%3Ai","type":"rich_text","rich_text":{"plain_text":"second","annotations":{"bold":true}}}],"has_more":false,"property_item":{"id":"r%3Ai","type":"rich_text"}}`), nil
				}
				return notionResponse(200, `{"object":"list","results":[{"object":"property_item","id":"r%3Ai","type":"rich_text","rich_text":{"plain_text":"first "}}],"has_more":true,"next_cursor":"notes-next","property_item":{"id":"r%3Ai","type":"rich_text"}}`), nil
			}
			if prop == "owner" {
				return notionResponse(200, fmt.Sprintf(`{"object":"list","results":[{"object":"property_item","id":"owner","type":"relation","relation":{"id":%q}}],"has_more":false,"property_item":{"id":"owner","type":"relation"}}`, notionRecordB)), nil
			}
			if prop == "calc" {
				return notionResponse(200, `{"object":"property_item","id":"calc","type":"formula","formula":{"type":"number","number":9999007199254740993.000005}}`), nil
			}
			if prop == "files" {
				f.refreshCalls++
				return notionResponse(200, `{"object":"property_item","id":"files","type":"files","files":[{"name":"invoice.txt","type":"file","file":{"url":"https://prod-files-secure.s3.us-west-2.amazonaws.com/invoice?fresh=1"}},{"name":"license.txt","type":"file","file":{"url":"https://prod-files-secure.s3.us-west-2.amazonaws.com/license?fresh=1"}}]}`), nil
			}
		}
		if path == "/v1/pages/"+notionTemplateA {
			return notionResponse(200, fmt.Sprintf(`{"object":"page","id":%q,"parent":{"data_source_id":%q},"properties":{"Name":{"id":"title","type":"title","title":[{"plain_text":"Planning template"}]},"Price":{"id":"price","type":"number","number":45.25}}}`, notionTemplateA, notionSourceA)), nil
		}
		if path == "/v1/pages/"+notionRecordA {
			price := "9007199254740993.000001"
			if f.changed {
				price = "42.5"
			}
			return notionResponse(200, fmt.Sprintf(`{"object":"page","id":%q,"parent":{"data_source_id":%q},"properties":{"Name":{"id":"title","type":"title","title":[{"plain_text":"truncated"}]},"Price":{"id":"price","type":"number","number":%s},"Notes":{"id":"r%%3Ai","type":"rich_text","rich_text":[{"plain_text":"truncated"}]},"Owner":{"id":"owner","type":"relation","relation":[]},"Total":{"id":"calc","type":"formula","formula":{"type":"number","number":-1}},"Attachments":{"id":"files","type":"files","files":[]}}}`, notionRecordA, notionSourceA, price)), nil
		}
		if path == "/v1/pages/"+notionRecordB {
			return notionResponse(200, fmt.Sprintf(`{"object":"page","id":%q,"parent":{"data_source_id":%q},"properties":{"Name":{"id":"title","type":"title","title":[{"plain_text":"Asha"}]}}}`, notionRecordB, notionSourceB)), nil
		}
		t.Fatalf("Unexpected Notion fixture request %s %s", r.Method, r.URL)
		return nil, nil
	})}
}
func startNotionDatabaseFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext) NotionCapture {
	t.Helper()
	out, err := startNotionCapture(context.Background(), k, e, map[string]any{"roots": []any{map[string]any{"kind": "database", "id": notionDBA}, map[string]any{"kind": "database", "id": notionDBB}}, "sourceNamespace": "notion-database-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return out.(NotionCapture)
}
func runNotionDatabaseFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, c NotionCapture, f *notionDatabaseFixture) NotionCapture {
	t.Helper()
	for i := 0; i < 150 && c.State != "ready"; i++ {
		var err error
		c, err = k.CaptureNotionStep(context.Background(), e, c.ID, c.Revision, "fixture-secret-token", f.client(t))
		if err != nil {
			t.Fatal(err)
		}
		if c.State == "blocked" {
			return c
		}
	}
	return c
}
func finishNotionDatabaseFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, c NotionCapture, partial bool) ImportPlan {
	t.Helper()
	out, err := finishNotionCapture(context.Background(), k, e, map[string]any{"captureId": c.ID, "revision": float64(c.Revision), "allowPartial": partial})
	if err != nil {
		t.Fatal(err)
	}
	return out.(ImportPlan)
}
func TestNotionDatabaseSourcesLinkedViewsAndResumableProperties(t *testing.T) {
	k, e := testKernel(t)
	f := &notionDatabaseFixture{denyProperty: true}
	capture := runNotionDatabaseFixture(t, k, e, startNotionDatabaseFixture(t, k, e), f)
	if capture.State != "blocked" || len(capture.Pending) == 0 || capture.Pending[0].Kind != "property" {
		t.Fatal("property cursor not retained", capture.State)
	}
	pending := capture.Pending[0]
	persisted, err := loadNotionCapture(context.Background(), k.db, capture.ID)
	if err != nil || persisted.Pending[0] != pending {
		t.Fatal("capture did not persist exact property cursor", err)
	}
	workspacePath := k.Path()
	k.Close()
	k, err = Open(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	if _, err = k.CaptureNotionStep(context.Background(), e, capture.ID, capture.Revision-1, "fixture-secret-token", f.client(t)); err == nil {
		t.Fatal("stale capture revision accepted after reopen")
	}
	if strings.Contains(string(marshal(persisted)), "fixture-secret-token") {
		t.Fatal("token persisted")
	}
	if err = contracts.Validate("service", "NotionCaptureResult", marshal(notionCaptureSummary(persisted))); err != nil {
		t.Fatal(err)
	}
	if _, exists := notionCaptureSummary(persisted)["objects"]; exists {
		t.Fatal("raw state escaped bounded IPC summary")
	}
	f.denyProperty = false
	capture = runNotionDatabaseFixture(t, k, e, persisted, f)
	if capture.State != "blocked" || capture.Pending[0].Kind != "attachment" {
		t.Fatal("expired attachment must retain refresh task", capture.State)
	}
	capture = runNotionDatabaseFixture(t, k, e, capture, f)
	if capture.State != "ready" {
		t.Fatal(capture.State)
	}
	if f.refreshCalls != 4 {
		t.Fatal("source URL was not refreshed for retry", f.refreshCalls)
	}
	plan := finishNotionDatabaseFixture(t, k, e, capture, false)
	if len(plan.PortableCollections) != 2 || len(plan.NotionContainers) != 2 {
		t.Fatal("sources merged or containers lost")
	}
	templates := 0
	for _, item := range plan.Items {
		if item.Kind == "notion_template" {
			templates++
			if !strings.Contains(string(marshal(item.Blocks)), "Template body offline") || !strings.Contains(string(marshal(item.Provenance)), "45.25") {
				t.Fatal("template lost its body/defaults", item)
			}
		}
	}
	if templates != 1 {
		t.Fatal("template identity lost", templates)
	}
	plan = completeImportFixture(t, k, e, plan)
	count := func(query string, want int) {
		t.Helper()
		var n int
		if err := k.db.QueryRow(query).Scan(&n); err != nil || n != want {
			t.Fatalf("%s got=%d want=%d err=%v", query, n, want, err)
		}
	}
	count("SELECT count(*) FROM collections", 2)
	count("SELECT count(*) FROM advanced_templates", 0)
	count("SELECT count(*) FROM records", 2)
	count("SELECT count(*) FROM containers", 2)
	count("SELECT count(*) FROM container_collections", 3)
	count("SELECT count(DISTINCT collection_id) FROM views", 1)
	count("SELECT count(*) FROM views WHERE container_id IS NOT NULL", 2)
	var decimal, notes, relation, cache, formula, title string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE property_type='number'").Scan(&decimal)
	if !strings.Contains(decimal, "9007199254740993.000001") {
		t.Fatal("precision lost", decimal)
	}
	k.db.QueryRow("SELECT value_json FROM property_values WHERE property_type='text'").Scan(&notes)
	if !strings.Contains(notes, "first ") || !strings.Contains(notes, "second") || strings.Contains(notes, "truncated") {
		t.Fatal("pagination lost", notes)
	}
	k.db.QueryRow("SELECT value_json FROM property_values WHERE property_type='relation'").Scan(&relation)
	var target string
	k.db.QueryRow("SELECT r.id FROM records r JOIN pages p ON p.id=r.page_id WHERE p.title='Asha'").Scan(&target)
	if relation != "[\""+target+"\"]" {
		t.Fatal("relation target not canonical", relation, target)
	}
	k.db.QueryRow("SELECT value_json FROM property_values WHERE property_type='formula'").Scan(&cache)
	if err = k.db.QueryRow("SELECT config_json FROM properties WHERE type='formula'").Scan(&formula); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cache, `"importedCachedValue"`) || !strings.Contains(cache, "9999007199254740993.000005") || !strings.Contains(formula, "originalExpression") || strings.Contains(formula, `"evaluatorVersion":2`) {
		t.Fatal("formula/cache conflated", cache, formula)
	}
	k.db.QueryRow("SELECT p.title FROM pages p JOIN records r ON r.page_id=p.id JOIN collections c ON c.id=r.collection_id WHERE c.title='Projects'").Scan(&title)
	if title != "Migration" {
		t.Fatal("full title lost", title)
	}
	count("SELECT count(*) FROM files WHERE content_hash='"+importDigest([]byte("offline invoice"))+"'", 1)
	count("SELECT count(*) FROM files WHERE content_hash='"+importDigest([]byte("offline license"))+"'", 1)
	var files string
	if err = k.db.QueryRow("SELECT value_json FROM property_values WHERE property_type='files'").Scan(&files); err != nil {
		t.Fatal(err)
	}
	var refs []any
	if err = json.Unmarshal([]byte(files), &refs); err != nil || len(refs) != 2 {
		t.Fatal("attachment task dedup lost a file", files, err)
	}
	// Recapture identical source without account access during commit. Same local
	// graph survives, and the raw review inventory remains independently retained.
	second := runNotionDatabaseFixture(t, k, e, startNotionDatabaseFixture(t, k, e), f)
	secondPlan := finishNotionDatabaseFixture(t, k, e, second, false)
	for _, item := range secondPlan.Items {
		if notionGraphPage(item) && item.Choice != "keep_local" {
			t.Fatalf("identical graph was not stable: %s %s %s", item.Locator, item.Choice, item.Conflict)
		}
	}
	completeImportFixture(t, k, e, secondPlan)
	count("SELECT count(*) FROM records", 2)
	count("SELECT count(*) FROM views", 2)
	// Changed incoming schema/values require a connected keep/copy decision.
	f.changed = true
	changed := finishNotionDatabaseFixture(t, k, e, runNotionDatabaseFixture(t, k, e, startNotionDatabaseFixture(t, k, e), f), false)
	if _, err = commitPortableImport(context.Background(), k, e, map[string]any{"planId": changed.ID, "planHash": changed.Hash}); err == nil {
		t.Fatal("changed graph committed without review")
	}
	choices := []any{}
	for _, item := range changed.Items {
		if notionGraphPage(item) {
			choices = append(choices, map[string]any{"sourceLocator": item.Locator, "choice": "duplicate"})
		}
	}
	configured, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": changed.ID, "planHash": changed.Hash, "choices": choices})
	if err != nil {
		t.Fatal(err)
	}
	changed = configured.(ImportPlan)
	completeImportFixture(t, k, e, changed)
	count("SELECT count(*) FROM records", 4)
	count("SELECT count(*) FROM collections", 4)
}
func TestNotionPartialPropertyAndUnsupportedViewNeverInventValues(t *testing.T) {
	k, e := testKernel(t)
	f := &notionDatabaseFixture{denyProperty: true}
	c := runNotionDatabaseFixture(t, k, e, startNotionDatabaseFixture(t, k, e), f)
	p := finishNotionDatabaseFixture(t, k, e, c, true)
	partial := false
	for _, i := range p.Items {
		if strings.Contains(i.Reason, "pagination") && i.Outcome == "partial" {
			partial = true
		}
	}
	if !partial {
		t.Fatal("missing field-level incomplete outcome")
	}
	for _, collection := range p.PortableCollections {
		for _, record := range collection.Records {
			for _, value := range record.Values {
				if strings.Contains(string(marshal(value)), "truncated") {
					t.Fatal("activated truncated property")
				}
			}
		}
	}
	v, reason := notionView(map[string]any{"type": "table", "filter": map[string]any{"property": "missing", "date": map[string]any{"past_week": map[string]any{}}}}, map[string]string{}, nil, nil, "")
	if v.Type != "unsupported_external" || !strings.Contains(reason, "without broadening") {
		t.Fatal(v, reason)
	}
}
func TestNotionPropertyURLAndNumericPrecision(t *testing.T) {
	endpoint, _, err := notionEndpoint(notionCaptureTask{Kind: "property", ID: notionRecordA, PropertyID: "r%3Ai", Cursor: "+/opaque"})
	if err != nil || strings.Contains(endpoint, "%253A") || !strings.Contains(endpoint, "start_cursor=%2B%2Fopaque") {
		t.Fatal(endpoint, err)
	}
	for input, want := range map[string]string{"1.234567890123456789e3": "1234.567890123456789", "1e-4": "0.0001", "-2e2": "-200", "0.123e5": "12300", "0e5": "0"} {
		got, err := notionDecimal(input)
		if err != nil || got != want {
			t.Fatal(input, got, err)
		}
	}
	var raw map[string]any
	json.Unmarshal([]byte(`{"roots":[{"kind":"database","id":"not-a-uuid"}]}`), &raw)
	if _, err := notionRoots(raw); err == nil {
		t.Fatal("invalid root accepted")
	}
}

func TestNotionDeniedQueryIsNotAnEmptyDatabase(t *testing.T) {
	k, e := testKernel(t)
	f := &notionDatabaseFixture{}
	base := f.client(t).Transport
	client := &http.Client{Transport: notionFixtureTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/query") {
			return notionResponse(403, `{"code":"restricted_resource"}`), nil
		}
		return base.RoundTrip(r)
	})}
	c := startNotionDatabaseFixture(t, k, e)
	for i := 0; i < 30; i++ {
		var err error
		c, err = k.CaptureNotionStep(context.Background(), e, c.ID, c.Revision, "fixture-token", client)
		if err != nil {
			t.Fatal(err)
		}
		if c.State == "blocked" {
			break
		}
	}
	if c.State != "blocked" || c.Pending[0].Kind != "query" {
		t.Fatal("denied query lost cursor")
	}
	p := finishNotionDatabaseFixture(t, k, e, c, true)
	if len(p.PortableCollections) != 0 {
		t.Fatal("denied record queries became empty canonical databases")
	}
	evidence := false
	for _, item := range p.Items {
		if strings.Contains(item.Reason, "misleading empty database") && item.Outcome == "partial" {
			evidence = true
		}
	}
	if !evidence {
		t.Fatal("missing explicit source completeness warning")
	}
	completeImportFixture(t, k, e, p)
	var n int
	if err := k.db.QueryRow("SELECT count(*) FROM collections").Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
func TestNotionRepeatedPropertyCursorAndLegacyVersion(t *testing.T) {
	k, e := testKernel(t)
	f := &notionDatabaseFixture{fileDenied: true}
	base := f.client(t).Transport
	client := &http.Client{Transport: notionFixtureTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/properties/r:i") {
			return notionResponse(200, `{"object":"list","results":[{"object":"property_item","id":"r%3Ai","type":"rich_text","rich_text":{"plain_text":"only once"}}],"has_more":true,"next_cursor":"repeat","property_item":{"id":"r%3Ai","type":"rich_text"}}`), nil
		}
		return base.RoundTrip(r)
	})}
	c := startNotionDatabaseFixture(t, k, e)
	failed := false
	for i := 0; i < 100; i++ {
		before := c
		next, err := k.CaptureNotionStep(context.Background(), e, c.ID, c.Revision, "fixture-token", client)
		if err != nil {
			if !strings.Contains(err.Error(), "repeated") {
				t.Fatal(err)
			}
			saved, loadErr := loadNotionCapture(context.Background(), k.db, c.ID)
			if loadErr != nil || saved.Revision != before.Revision {
				t.Fatal("invalid cursor advanced persisted state", loadErr)
			}
			prop := saved.Objects["record:"+notionRecordA].Properties["r%3Ai"]
			if len(prop.Items) != 1 || prop.Complete {
				t.Fatal("repeated prefix treated as complete", prop)
			}
			failed = true
			break
		}
		c = next
	}
	if !failed {
		t.Fatal("repeated cursor not rejected")
	}
	legacy := startNotionDatabaseFixture(t, k, e)
	legacy.APIVersion = ""
	legacy.Pending = []notionCaptureTask{{Kind: "page", ID: notionRecordA, PageID: NewID(), ParentID: NewID()}}
	if err := k.write(context.Background(), func(tx *sql.Tx) error { return saveNotionCapture(context.Background(), tx, legacy, legacy.Revision) }); err != nil {
		t.Fatal(err)
	}
	legacyClient := &http.Client{Transport: notionFixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Notion-Version") != "2025-09-03" {
			t.Fatal("legacy capture silently upgraded", r.Header)
		}
		return notionResponse(200, fmt.Sprintf(`{"id":%q,"properties":{}}`, notionRecordA)), nil
	})}
	if _, err := k.CaptureNotionStep(context.Background(), e, legacy.ID, legacy.Revision, "fixture-token", legacyClient); err != nil {
		t.Fatal(err)
	}
}
func TestNotionNativeViewMappingsAndInertUnsupportedSettings(t *testing.T) {
	title, date, status, files := NewID(), NewID(), NewID(), NewID()
	collection := NewID()
	ids := map[string]string{"title": title, "due": date, "status": status, "files": files}
	props := []contracts.DomainPropertyDefinition{{Id: contracts.DomainId(title), CollectionId: contracts.DomainId(collection), Type: "title"}, {Id: contracts.DomainId(date), CollectionId: contracts.DomainId(collection), Type: "date"}, {Id: contracts.DomainId(status), CollectionId: contracts.DomainId(collection), Type: "status"}, {Id: contracts.DomainId(files), CollectionId: contracts.DomainId(collection), Type: "files"}}
	for kind, cfg := range map[string]map[string]any{"table": {"properties": []any{map[string]any{"property_id": "title", "visible": true, "width": json.Number("240")}}}, "list": {}, "board": {"group_by": map[string]any{"property_id": "status", "type": "status", "group_by": "option", "hide_empty_groups": true}}, "calendar": {"date_property_id": "due"}, "timeline": {"date_property_id": "due", "preference": map[string]any{"zoom_level": "quarter"}}, "gallery": {"cover": map[string]any{"type": "property", "property_id": "files"}, "cover_size": "large"}} {
		t.Run(kind, func(t *testing.T) {
			view, _ := notionView(map[string]any{"type": kind, "configuration": cfg}, ids, props, nil, "")
			if view.Type != kind {
				t.Fatal(view.Config)
			}
			if err := contracts.Validate("domain", "view_"+kind+"_v1", marshal(view.Config)); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "calendar", "timeline":
				if view.Config["datePropertyId"] != date {
					t.Fatal(view.Config)
				}
			case "board":
				if view.Config["groupPropertyId"] != status || view.Config["hideEmptyGroups"] != true {
					t.Fatal(view.Config)
				}
			case "gallery":
				if view.Config["coverPropertyId"] != files {
					t.Fatal(view.Config)
				}
			}
		})
	}
	for _, source := range []map[string]any{{"type": "chart"}, {"type": "timeline", "configuration": map[string]any{"date_property_id": "due", "end_date_property_id": "status"}}, {"type": "table", "quick_filters": map[string]any{"status": map[string]any{"equals": "me"}}}} {
		view, _ := notionView(source, ids, props, nil, "")
		if view.Type != "unsupported_external" {
			t.Fatal("unsupported result semantics activated", source)
		}
	}
}

func TestNotionOverlappingPageAndDatabaseRootsShareIdentity(t *testing.T) {
	k, e := testKernel(t)
	f := &notionDatabaseFixture{fileDenied: true}
	out, err := startNotionCapture(context.Background(), k, e, map[string]any{"roots": []any{map[string]any{"kind": "page", "id": notionRecordA}, map[string]any{"kind": "database", "id": notionDBA}}, "sourceNamespace": "overlap"})
	if err != nil {
		t.Fatal(err)
	}
	c := runNotionDatabaseFixture(t, k, e, out.(NotionCapture), f)
	if c.State != "ready" {
		t.Fatal(c.State, c.Error)
	}
	p := finishNotionDatabaseFixture(t, k, e, c, false)
	count := 0
	for _, item := range p.Items {
		if item.PageID != "" && item.ExternalID == notionRecordA {
			count++
		}
	}
	if count != 1 {
		t.Fatal("overlapping root created multiple pages", count)
	}
	completeImportFixture(t, k, e, p)
	var records int
	if err = k.db.QueryRow("SELECT count(*) FROM records").Scan(&records); err != nil || records != 2 {
		t.Fatal("overlapping page lost its record ownership", records, err)
	}
}

func TestNotionSourceIdentityAppearingAfterPreviewRejectsBeforeWrites(t *testing.T) {
	k, e := testKernel(t)
	f := &notionDatabaseFixture{fileDenied: true}
	first := finishNotionDatabaseFixture(t, k, e, runNotionDatabaseFixture(t, k, e, startNotionDatabaseFixture(t, k, e), f), false)
	second := finishNotionDatabaseFixture(t, k, e, runNotionDatabaseFixture(t, k, e, startNotionDatabaseFixture(t, k, e), f), false)
	completeImportFixture(t, k, e, first)
	if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": second.ID, "planHash": second.Hash}); err == nil || !strings.Contains(err.Error(), "after this preview") {
		t.Fatal("stale source identity was accepted", err)
	}
	var pages int
	if err := k.db.QueryRow("SELECT count(*) FROM pages WHERE id=?", second.RootID).Scan(&pages); err != nil || pages != 0 {
		t.Fatal("stale preview wrote canonical pages", pages, err)
	}
}
