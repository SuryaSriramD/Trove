package auth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/google/uuid"
)

const secretServiceName = "org.freedesktop.secrets"
const secretRoot dbus.ObjectPath = "/org/freedesktop/secrets"
const secretInterface = "org.freedesktop.Secret."
const secretAlgorithm = "dh-ietf1024-sha256-aes128-cbc-pkcs7"

type secretWire struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}
type secretServiceStore struct {
	namespace string
	connect   func(context.Context) (*dbus.Conn, error)
}
type secretSession struct {
	conn  *dbus.Conn
	owner string
	path  dbus.ObjectPath
	key   []byte
}

func newSecretServiceStore(root string, connect func(context.Context) (*dbus.Conn, error)) *secretServiceStore {
	sum := sha256.Sum256([]byte(root))
	return &secretServiceStore{namespace: fmt.Sprintf("org.trove.siwc.%x", sum[:16]), connect: connect}
}
func secretFailure() error {
	return fail("CREDENTIAL_STORE_FAILED", "The desktop Secret Service could not complete the protected account operation")
}
func secretObject(path dbus.ObjectPath) bool {
	return path.IsValid() && path != "/" && strings.HasPrefix(string(path), "/org/freedesktop/secrets/")
}
func (s *secretSession) call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) *dbus.Call {
	return s.conn.Object(s.owner, path).CallWithContext(ctx, secretInterface+method, 0, args...)
}
func (s *secretSession) close() {
	if secretObject(s.path) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = s.call(ctx, s.path, "Session.Close").Err
		cancel()
	}
	clear(s.key)
	_ = s.conn.Close()
}
func (s *secretServiceStore) session(ctx context.Context) (*secretSession, error) {
	conn, err := s.connect(ctx)
	if err != nil {
		return nil, secretFailure()
	}
	session := &secretSession{conn: conn}
	ok := false
	defer func() {
		if !ok {
			session.close()
		}
	}()
	// An already-running owner need not have an activatable .service file.
	// Start only when no owner can be resolved, then pin that unique owner.
	if err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, secretServiceName).Store(&session.owner); err != nil {
		var started uint32
		if conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.StartServiceByName", 0, secretServiceName, uint32(0)).Store(&started) != nil || conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, secretServiceName).Store(&session.owner) != nil {
			return nil, secretFailure()
		}
	}
	if !strings.HasPrefix(session.owner, ":") || len(session.owner) > 256 {
		return nil, secretFailure()
	}
	private, public, err := secretKeypair()
	if err != nil {
		return nil, secretFailure()
	}
	defer func() { clear(private.Bits()); private.SetInt64(0) }()
	var output dbus.Variant
	if session.call(ctx, secretRoot, "Service.OpenSession", secretAlgorithm, dbus.MakeVariant(public)).Store(&output, &session.path) != nil || !secretObject(session.path) {
		return nil, secretFailure()
	}
	peer, valid := output.Value().([]byte)
	if !valid {
		return nil, secretFailure()
	}
	session.key, err = secretSharedKey(private, peer)
	if err != nil {
		return nil, secretFailure()
	}
	ok = true
	return session, nil
}
func (s *secretServiceStore) attributes(id string) (map[string]string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return nil, fail("CREDENTIAL_STORE_FAILED", "Credential identity is invalid")
	}
	return map[string]string{"application": s.namespace, "registration": id, "schema": "org.trove.siwc.v1"}, nil
}
func (s *secretSession) prompt(ctx context.Context, path dbus.ObjectPath) (dbus.Variant, error) {
	if path == "/" {
		return dbus.Variant{}, nil
	}
	if !secretObject(path) {
		return dbus.Variant{}, secretFailure()
	}
	signals := make(chan *dbus.Signal, 8)
	s.conn.Signal(signals)
	defer s.conn.RemoveSignal(signals)
	options := []dbus.MatchOption{dbus.WithMatchSender(s.owner), dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(secretInterface + "Prompt"), dbus.WithMatchMember("Completed")}
	if err := s.conn.AddMatchSignalContext(ctx, options...); err != nil {
		return dbus.Variant{}, secretFailure()
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.conn.RemoveMatchSignalContext(cleanup, options...)
	}()
	completed := false
	defer func() {
		if !completed {
			cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = s.call(cleanup, path, "Prompt.Dismiss").Err
			cancel()
		}
	}()
	if s.call(ctx, path, "Prompt.Prompt", "").Err != nil {
		if ctx.Err() != nil {
			return dbus.Variant{}, ctx.Err()
		}
		return dbus.Variant{}, secretFailure()
	}
	for {
		select {
		case <-ctx.Done():
			return dbus.Variant{}, ctx.Err()
		case signal, open := <-signals:
			if !open {
				return dbus.Variant{}, secretFailure()
			}
			if signal.Sender != s.owner || signal.Path != path || signal.Name != secretInterface+"Prompt.Completed" {
				continue
			}
			var dismissed bool
			var result dbus.Variant
			if dbus.Store(signal.Body, &dismissed, &result) != nil {
				return dbus.Variant{}, secretFailure()
			}
			completed = true
			if dismissed {
				return dbus.Variant{}, fail("CREDENTIAL_STORE_CANCELLED", "The desktop credential prompt was cancelled")
			}
			return result, nil
		}
	}
}
func (s *secretSession) unlock(ctx context.Context, path dbus.ObjectPath) error {
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if s.call(ctx, secretRoot, "Service.Unlock", []dbus.ObjectPath{path}).Store(&unlocked, &prompt) != nil {
		return secretFailure()
	}
	if prompt != "/" {
		result, err := s.prompt(ctx, prompt)
		if err != nil {
			return err
		}
		items, ok := result.Value().([]dbus.ObjectPath)
		if !ok {
			return secretFailure()
		}
		unlocked = append(unlocked, items...)
	}
	if len(unlocked) > 32 {
		return secretFailure()
	}
	for _, item := range unlocked {
		if item == path {
			return nil
		}
	}
	return fail("CREDENTIAL_STORE_LOCKED", "Unlock the desktop keyring to use this account")
}
func (s *secretSession) find(ctx context.Context, attrs map[string]string) (dbus.ObjectPath, error) {
	var unlocked, locked []dbus.ObjectPath
	if s.call(ctx, secretRoot, "Service.SearchItems", attrs).Store(&unlocked, &locked) != nil {
		return "", secretFailure()
	}
	if len(unlocked)+len(locked) == 0 {
		return "", ErrSecretMissing
	}
	if len(unlocked)+len(locked) != 1 {
		return "", fail("CREDENTIAL_STORE_FAILED", "Duplicate protected account records require keyring repair")
	}
	if len(locked) == 1 {
		if !secretObject(locked[0]) {
			return "", secretFailure()
		}
		if err := s.unlock(ctx, locked[0]); err != nil {
			return "", err
		}
		return locked[0], nil
	}
	if !secretObject(unlocked[0]) {
		return "", secretFailure()
	}
	return unlocked[0], nil
}
func (s *secretServiceStore) Get(ctx context.Context, id string) ([]byte, error) {
	attrs, err := s.attributes(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	session, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	defer session.close()
	item, err := session.find(ctx, attrs)
	if err != nil {
		return nil, err
	}
	var wire secretWire
	if session.call(ctx, item, "Item.GetSecret", session.path).Store(&wire) != nil || wire.Session != session.path || wire.ContentType != "application/octet-stream" {
		return nil, secretFailure()
	}
	data, err := decryptSecret(session.key, wire.Parameters, wire.Value)
	if err != nil {
		return nil, secretFailure()
	}
	if err = ctx.Err(); err != nil {
		clear(data)
		return nil, err
	}
	return data, nil
}
func (s *secretServiceStore) Put(ctx context.Context, id string, data []byte) error {
	attrs, err := s.attributes(id)
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 131072 {
		return fail("CREDENTIAL_STORE_FAILED", "Credential record is invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	session, err := s.session(ctx)
	if err != nil {
		return err
	}
	defer session.close()
	// Search first so duplicate registrations are never silently overwritten.
	existing, findErr := session.find(ctx, attrs)
	if findErr != nil && findErr != ErrSecretMissing {
		return findErr
	}
	iv, encrypted, err := encryptSecret(session.key, data)
	if err != nil {
		return secretFailure()
	}
	if findErr == nil {
		if session.call(ctx, existing, "Item.SetSecret", secretWire{session.path, iv, encrypted, "application/octet-stream"}).Err != nil {
			return secretFailure()
		}
		return nil
	}
	var collection dbus.ObjectPath
	if session.call(ctx, secretRoot, "Service.ReadAlias", "default").Store(&collection) != nil {
		return secretFailure()
	}
	if collection == "/" {
		var prompt dbus.ObjectPath
		properties := map[string]dbus.Variant{secretInterface + "Collection.Label": dbus.MakeVariant("Trove accounts")}
		if session.call(ctx, secretRoot, "Service.CreateCollection", properties, "default").Store(&collection, &prompt) != nil {
			return secretFailure()
		}
		if prompt != "/" {
			result, err := session.prompt(ctx, prompt)
			if err != nil {
				return err
			}
			var ok bool
			collection, ok = result.Value().(dbus.ObjectPath)
			if !ok {
				return secretFailure()
			}
		}
	}
	if !secretObject(collection) {
		return secretFailure()
	}
	if err := session.unlock(ctx, collection); err != nil {
		return err
	}
	properties := map[string]dbus.Variant{secretInterface + "Item.Label": dbus.MakeVariant("Trove account"), secretInterface + "Item.Attributes": dbus.MakeVariant(attrs)}
	var item, prompt dbus.ObjectPath
	if session.call(ctx, collection, "Collection.CreateItem", properties, secretWire{session.path, iv, encrypted, "application/octet-stream"}, true).Store(&item, &prompt) != nil {
		return secretFailure()
	}
	if prompt != "/" {
		result, err := session.prompt(ctx, prompt)
		if err != nil {
			return err
		}
		var ok bool
		item, ok = result.Value().(dbus.ObjectPath)
		if !ok {
			return secretFailure()
		}
	}
	if !secretObject(item) {
		return secretFailure()
	}
	return nil
}
func (s *secretServiceStore) Delete(ctx context.Context, id string) error {
	attrs, err := s.attributes(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	session, err := s.session(ctx)
	if err != nil {
		return err
	}
	defer session.close()
	item, err := session.find(ctx, attrs)
	if err == ErrSecretMissing {
		return nil
	}
	if err != nil {
		return err
	}
	var prompt dbus.ObjectPath
	if session.call(ctx, item, "Item.Delete").Store(&prompt) != nil {
		return secretFailure()
	}
	_, err = session.prompt(ctx, prompt)
	return err
}
