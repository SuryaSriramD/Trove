package auth

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/google/uuid"
)

type fakeSecretService struct {
	mu                                              sync.Mutex
	conn                                            *dbus.Conn
	keys                                            map[dbus.ObjectPath][]byte
	attrs                                           map[string]string
	data                                            []byte
	algorithms                                      []string
	ciphertext                                      [][]byte
	duplicates, wrongSession, locked, dismiss, wait bool
	missingDefault, rejectAlgorithm                 bool
	collectionsCreated                              int
	promptStarted                                   chan struct{}
	dismissals                                      int
}

const fakeCollection dbus.ObjectPath = "/org/freedesktop/secrets/collection/trove"
const fakeItem dbus.ObjectPath = "/org/freedesktop/secrets/collection/trove/item1"
const fakePrompt dbus.ObjectPath = "/org/freedesktop/secrets/prompt/test"

func (f *fakeSecretService) OpenSession(algorithm string, input dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.algorithms = append(f.algorithms, algorithm)
	peer, ok := input.Value().([]byte)
	if !ok || algorithm != secretAlgorithm || f.rejectAlgorithm {
		return dbus.Variant{}, "/", dbus.NewError("org.freedesktop.DBus.Error.NotSupported", nil)
	}
	private, public, err := secretKeypair()
	if err != nil {
		return dbus.Variant{}, "/", dbus.MakeFailedError(err)
	}
	defer clear(private.Bits())
	key, err := secretSharedKey(private, peer)
	if err != nil {
		return dbus.Variant{}, "/", dbus.MakeFailedError(err)
	}
	path := dbus.ObjectPath("/org/freedesktop/secrets/session/s" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	f.keys[path] = key
	return dbus.MakeVariant(public), path, nil
}
func (f *fakeSecretService) Close() *dbus.Error { return nil }
func (f *fakeSecretService) ReadAlias(string) (dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.missingDefault {
		return "/", nil
	}
	return fakeCollection, nil
}
func (f *fakeSecretService) CreateCollection(_ map[string]dbus.Variant, alias string) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if alias != "default" {
		return "/", "/", dbus.MakeFailedError(fmt.Errorf("wrong alias"))
	}
	f.missingDefault = false
	f.collectionsCreated++
	return fakeCollection, "/", nil
}
func (f *fakeSecretService) SearchItems(attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data == nil {
		return nil, nil, nil
	}
	for k, v := range attrs {
		if f.attrs[k] != v {
			return nil, nil, nil
		}
	}
	if f.duplicates {
		return []dbus.ObjectPath{fakeItem, fakeItem}, nil, nil
	}
	if f.locked {
		return nil, []dbus.ObjectPath{fakeItem}, nil
	}
	return []dbus.ObjectPath{fakeItem}, nil, nil
}
func (f *fakeSecretService) Unlock(paths []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.locked {
		return nil, fakePrompt, nil
	}
	return paths, "/", nil
}
func (f *fakeSecretService) Prompt(string) *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.promptStarted != nil {
		select {
		case f.promptStarted <- struct{}{}:
		default:
		}
	}
	if f.wait {
		return nil
	}
	if !f.dismiss {
		f.locked = false
	}
	_ = f.conn.Emit(fakePrompt, secretInterface+"Prompt.Completed", f.dismiss, dbus.MakeVariant([]dbus.ObjectPath{fakeItem, fakeCollection}))
	return nil
}
func (f *fakeSecretService) Dismiss() *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dismissals++
	return nil
}
func (f *fakeSecretService) CreateItem(props map[string]dbus.Variant, secret secretWire, _ bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	attrs, ok := props[secretInterface+"Item.Attributes"].Value().(map[string]string)
	if !ok {
		return "/", "/", dbus.MakeFailedError(fmt.Errorf("attributes"))
	}
	f.attrs = attrs
	if err := f.accept(secret); err != nil {
		return "/", "/", dbus.MakeFailedError(err)
	}
	return fakeItem, "/", nil
}
func (f *fakeSecretService) accept(secret secretWire) error {
	data, err := decryptSecret(f.keys[secret.Session], secret.Parameters, secret.Value)
	if err != nil {
		return err
	}
	f.data = data
	f.ciphertext = append(f.ciphertext, append([]byte(nil), secret.Value...))
	return nil
}
func (f *fakeSecretService) SetSecret(secret secretWire) *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.accept(secret); err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}
func (f *fakeSecretService) GetSecret(path dbus.ObjectPath) (secretWire, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	iv, data, err := encryptSecret(f.keys[path], f.data)
	if err != nil {
		return secretWire{}, dbus.MakeFailedError(err)
	}
	if f.wrongSession {
		path = "/org/freedesktop/secrets/session/wrong"
	}
	return secretWire{path, iv, data, "application/octet-stream"}, nil
}
func (f *fakeSecretService) Delete() (dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	clear(f.data)
	f.data = nil
	return "/", nil
}
func privateSecretFixture(t *testing.T) (*secretServiceStore, *fakeSecretService) {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("private D-Bus daemon unavailable; real Linux qualification is separate")
	}
	base, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	temp, err := os.MkdirTemp(base, "tsb-")
	if err == nil {
		t.Cleanup(func() { _ = os.RemoveAll(temp) })
	}
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, daemon, "--session", "--nofork", "--print-address=1", "--address=unix:path="+filepath.Join(temp, "bus"))
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	addressCh := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); addressCh <- strings.TrimSpace(line) }()
	var address string
	select {
	case address = <-addressCh:
	case <-time.After(3 * time.Second):
		t.Fatal("private D-Bus startup timed out")
	}
	if !strings.HasPrefix(address, "unix:path="+temp) {
		t.Fatal("unexpected fixture address", address, stderr.String())
	}
	connect := func(context.Context) (*dbus.Conn, error) {
		return dbus.Connect(address, dbus.WithAuth(dbus.AuthExternal(strconv.Itoa(os.Geteuid()))), dbus.WithIncomingInterceptor(func(message *dbus.Message) {
			if message.Type == dbus.TypeError {
				t.Logf("Synthetic bus error: %v %v", message.Headers[dbus.FieldErrorName], message.Body)
			}
		}))
	}
	server, err := connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	reply, err := server.RequestName(secretServiceName, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatal(reply, err)
	}
	fake := &fakeSecretService{conn: server, keys: map[dbus.ObjectPath][]byte{}}
	for path, intf := range map[dbus.ObjectPath]string{secretRoot: "Service", fakeCollection: "Collection", fakeItem: "Item", fakePrompt: "Prompt"} {
		if err = server.Export(fake, path, secretInterface+intf); err != nil {
			t.Fatal(err)
		}
	}
	if err = server.ExportSubtree(fake, "/org/freedesktop/secrets/session", secretInterface+"Session"); err != nil {
		t.Fatal(err)
	}
	return newSecretServiceStore(temp, connect), fake
}
func TestSecretServicePrivateBusRoundTripAndRotation(t *testing.T) {
	store, fake := privateSecretFixture(t)
	ctx := context.Background()
	id := uuid.NewString()
	data := []byte("synthetic credential one")
	if err := store.Put(ctx, id, data); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, id)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("get", err)
	}
	rotated := []byte("synthetic credential rotated")
	if err := store.Put(ctx, id, rotated); err != nil {
		t.Fatal(err)
	}
	got, err = store.Get(ctx, id)
	if err != nil || !bytes.Equal(got, rotated) {
		t.Fatal("rotation", err)
	}
	if _, err = store.Get(ctx, uuid.NewString()); err != ErrSecretMissing {
		t.Fatal("registration isolation", err)
	}
	fake.mu.Lock()
	for _, algorithm := range fake.algorithms {
		if algorithm != secretAlgorithm {
			t.Fatal("plaintext algorithm")
		}
	}
	for _, wire := range fake.ciphertext {
		if bytes.Contains(wire, []byte("synthetic credential")) {
			t.Fatal("plaintext credential on wire")
		}
	}
	fake.mu.Unlock()
	if err = store.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err = store.Delete(ctx, id); err != nil {
		t.Fatal("idempotent delete", err)
	}
	if _, err = store.Get(ctx, id); err != ErrSecretMissing {
		t.Fatal("deleted credential returned", err)
	}
}
func TestSecretServiceRefusesDuplicatesWrongSessionAndDismissedPrompt(t *testing.T) {
	store, fake := privateSecretFixture(t)
	ctx := context.Background()
	id := uuid.NewString()
	if err := store.Put(ctx, id, []byte("synthetic credential")); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.duplicates = true
	fake.mu.Unlock()
	if _, err := store.Get(ctx, id); err == nil {
		t.Fatal("duplicate accepted")
	}
	fake.mu.Lock()
	fake.duplicates = false
	fake.wrongSession = true
	fake.mu.Unlock()
	if _, err := store.Get(ctx, id); err == nil {
		t.Fatal("wrong crypto session accepted")
	}
	fake.mu.Lock()
	fake.wrongSession = false
	fake.locked = true
	fake.dismiss = true
	fake.mu.Unlock()
	if _, err := store.Get(ctx, id); errorCode(err) != "CREDENTIAL_STORE_CANCELLED" {
		t.Fatal("dismissal", err)
	}
	fake.mu.Lock()
	fake.dismiss = false
	fake.mu.Unlock()
	if _, err := store.Get(ctx, id); err != nil {
		t.Fatal("native unlock", err)
	}
}
func TestSecretServiceCancellationDismissesOnlyItsPrompt(t *testing.T) {
	store, fake := privateSecretFixture(t)
	id := uuid.NewString()
	if err := store.Put(context.Background(), id, []byte("synthetic credential")); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.locked = true
	fake.wait = true
	fake.promptStarted = make(chan struct{}, 1)
	started := fake.promptStarted
	fake.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := store.Get(ctx, id); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("prompt never started")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled secret returned")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled operation did not finish")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.dismissals != 1 {
		t.Fatal("native prompt was not dismissed", fake.dismissals)
	}
}

func TestSecretServiceCreatesMissingDefaultAndNeverFallsBackToPlain(t *testing.T) {
	store, fake := privateSecretFixture(t)
	fake.mu.Lock()
	fake.missingDefault = true
	fake.mu.Unlock()
	if err := store.Put(context.Background(), uuid.NewString(), []byte("synthetic initial account")); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	if fake.collectionsCreated != 1 {
		t.Fatal("initial collection was not created")
	}
	fake.rejectAlgorithm = true
	fake.algorithms = nil
	fake.mu.Unlock()
	if _, err := store.Get(context.Background(), uuid.NewString()); err == nil {
		t.Fatal("unsupported encrypted transport accepted")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.algorithms) != 1 || fake.algorithms[0] != secretAlgorithm {
		t.Fatal("plaintext fallback attempted", fake.algorithms)
	}
}
