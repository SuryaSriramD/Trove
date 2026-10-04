package contracts

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestTypedBatchAndSchemaValidation(t *testing.T) {
	raw, err := os.ReadFile("examples/service-batch.json")
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeBatch(raw)
	if err != nil {
		t.Fatal(err)
	}
	command, ok := request.Commands[0].Value.(ServiceCreatePage)
	if !ok || command.Title != "Meeting notes" {
		t.Fatalf("wrong typed command: %#v", request.Commands[0])
	}
	round, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, raw, round)
	var invalid map[string]any
	json.Unmarshal(raw, &invalid)
	invalid["actor"] = "root"
	b, _ := json.Marshal(invalid)
	if _, err := DecodeBatch(b); err == nil {
		t.Fatal("accepted renderer-supplied actor")
	}
}
func assertSameJSON(t *testing.T, a, b []byte) {
	t.Helper()
	var x, y any
	json.Unmarshal(a, &x)
	json.Unmarshal(b, &y)
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("JSON roundtrip changed data\n%s\n%s", a, b)
	}
}
func TestAllProtocolFixtures(t *testing.T) {
	b, err := os.ReadFile("examples/protocol-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest []struct{ Path, Schema, Definition string }
	if err = json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, item := range manifest {
		t.Run(item.Path, func(t *testing.T) {
			raw, err := os.ReadFile("examples/" + item.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err = Validate(item.Schema, item.Definition, raw); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestDomainTypedRoundtrip(t *testing.T) {
	raw, err := os.ReadFile("examples/domain-workspace.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = Validate("domain", "DomainSnapshot", raw); err != nil {
		t.Fatal(err)
	}
	var snapshot DomainDomainSnapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, raw, out)
}
func TestMergedImportResolutionAndFilterRoundtrip(t *testing.T) {
	raw, err := os.ReadFile("examples/domain-import-resolution.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = Validate("domain", "ImportResolution", raw); err != nil {
		t.Fatal(err)
	}
	var value DomainImportResolution
	if err = json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, raw, out)
	filter := []byte(`{"op":"and","terms":[{"op":"eq","propertyId":"0199a000-0000-7000-8000-000000000002","value":"done"}]}`)
	if err = Validate("domain", "Filter", filter); err != nil {
		t.Fatal(err)
	}
	var f DomainFilter
	if err = json.Unmarshal(filter, &f); err != nil {
		t.Fatal(err)
	}
	out, err = json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, filter, out)
}

func TestOptionalNullableIntentSurvivesTypedCommands(t *testing.T) {
	for _, raw := range []string{
		`{"kind":"page.update","pageId":"0199a000-0000-7000-8000-000000000002"}`,
		`{"kind":"page.update","pageId":"0199a000-0000-7000-8000-000000000002","icon":null,"coverFileId":null}`,
		`{"kind":"page.update","pageId":"0199a000-0000-7000-8000-000000000002","icon":"🌱","coverFileId":"0199a000-0000-7000-8000-000000000003"}`,
	} {
		var command ServiceCommand
		if err := json.Unmarshal([]byte(raw), &command); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(command)
		if err != nil {
			t.Fatal(err)
		}
		assertSameJSON(t, []byte(raw), encoded)
	}
	command := ServiceUpdatePage{Kind: "page.update", PageId: "0199a000-0000-7000-8000-000000000002", CoverFileIdPresent: true}
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, []byte(`{"kind":"page.update","pageId":"0199a000-0000-7000-8000-000000000002","coverFileId":null}`), encoded)
}
