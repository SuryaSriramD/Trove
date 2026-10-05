package updates

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func wireFixture() rollbackMessage {
	return rollbackMessage{Version: 1, Kind: "ready", Nonce: strings.Repeat("ab", 32), PID: 8123, RollbackTarget: RollbackTarget{WorkspacePath: filepath.Join(os.TempDir(), "trove-handshake-fixture", "restored"), WorkspaceID: "workspace-reviewed", UserVersion: 3, StorageGeneration: 1}}
}
func frameRaw(raw []byte) []byte {
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], uint32(len(raw)))
	return append(h[:], raw...)
}

type fragmentedReader struct{ io.Reader }

func (r fragmentedReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}
func TestRollbackWirePreservesBoundedAuthority(t *testing.T) {
	expected := wireFixture()
	var b bytes.Buffer
	if err := writeRollbackMessage(&b, expected); err != nil {
		t.Fatal(err)
	}
	actual, err := readRollbackMessage(fragmentedReader{&b})
	if err != nil || actual != expected {
		t.Fatalf("wire changed authority: %#v %v", actual, err)
	}
	if !matchesRollbackMessage(actual, "ready", expected.Nonce, expected.PID, expected.RollbackTarget) {
		t.Fatal("matching authority refused")
	}
	for _, change := range []func(*rollbackMessage){func(m *rollbackMessage) { m.Nonce = strings.Repeat("01", 32) }, func(m *rollbackMessage) { m.PID++ }, func(m *rollbackMessage) { m.WorkspaceID = "another" }, func(m *rollbackMessage) { m.WorkspacePath = "/other" }, func(m *rollbackMessage) { m.UserVersion++ }, func(m *rollbackMessage) { m.StorageGeneration++ }, func(m *rollbackMessage) { m.Kind = "commit" }} {
		changed := actual
		change(&changed)
		if matchesRollbackMessage(changed, "ready", expected.Nonce, expected.PID, expected.RollbackTarget) {
			t.Fatal("changed authority accepted", changed)
		}
	}
}
func TestRollbackWireRejectsMalformedOrAmbiguousInput(t *testing.T) {
	raw, _ := json.Marshal(wireFixture())
	valid := string(raw)
	cases := [][]byte{nil, []byte("null"), []byte("[]"), []byte(valid + "{}"), []byte(strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1)), []byte(strings.Replace(valid, `"version":1`, `"unknown":1`, 1)), []byte(strings.Replace(valid, `"pid":8123`, `"pid":0`, 1)), []byte(strings.Replace(valid, `"pid":8123`, `"pid":null`, 1)), []byte(strings.Replace(valid, `"version":1`, `"version":2`, 1)), []byte(strings.Replace(valid, `"kind":"ready"`, `"kind":"other"`, 1)), []byte(strings.Replace(valid, `"workspaceId":"workspace-reviewed"`, `"workspaceId":{}`, 1)), []byte(strings.Repeat("x", rollbackFrameLimit+1)), append([]byte{255}, raw...)}
	for i, raw := range cases {
		if _, err := readRollbackMessage(bytes.NewReader(frameRaw(raw))); err == nil {
			t.Fatalf("malformed case %d accepted", i)
		}
	}
	framed := frameRaw(raw)
	for _, n := range []int{1, 3, 4, len(framed) - 1} {
		if _, err := readRollbackMessage(bytes.NewReader(framed[:n])); err == nil {
			t.Fatal("truncated frame accepted", n)
		}
	}
}
func TestRollbackNoncesAreIndependent(t *testing.T) {
	a, err := newRollbackNonce()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newRollbackNonce()
	if err != nil || len(a) != 64 || a == b {
		t.Fatal("invalid handoff nonce")
	}
}
