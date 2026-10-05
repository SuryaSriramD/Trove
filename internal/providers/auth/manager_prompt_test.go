package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type promptingStore struct {
	*memoryStore
	started, cancelled, release chan struct{}
	mode                        string
	writing                     string
	mu                          sync.Mutex
	deleted                     []string
	cleanupContextValid         bool
}

func (s *promptingStore) Put(ctx context.Context, id string, data []byte) error {
	s.mu.Lock()
	s.writing = id
	s.mu.Unlock()
	if s.mode == "stored_before_cancel" {
		if err := s.memoryStore.Put(ctx, id, data); err != nil {
			return err
		}
	}
	close(s.started)
	<-ctx.Done()
	close(s.cancelled)
	<-s.release
	if s.mode == "late_store" || s.mode == "late_store_error" || s.mode == "cleanup_failure" {
		// A native service may have committed before cancellation was noticed.
		if err := s.memoryStore.Put(context.Background(), id, data); err != nil {
			return err
		}
		if s.mode == "late_store_error" {
			return errors.New("synthetic native acknowledgement lost")
		}
		return nil
	}
	return ctx.Err()
}
func (s *promptingStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	s.deleted = append(s.deleted, id)
	deadline, ok := ctx.Deadline()
	s.cleanupContextValid = ctx.Err() == nil && ok && time.Until(deadline) > 0 && time.Until(deadline) <= 5*time.Second
	s.mu.Unlock()
	return s.memoryStore.Delete(ctx, id)
}

func waitPromptSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not finish promptly", name)
	}
}
func TestSIWCCancelOrCloseDuringCredentialPromptDoesNotPublish(t *testing.T) {
	for _, action := range []string{"cancel", "close", "caller_context"} {
		for _, mode := range []string{"cancel_aware", "stored_before_cancel", "late_store", "late_store_error", "cleanup_failure"} {
			t.Run(action+"/"+mode, func(t *testing.T) {
				f := newFixture(t)
				other := uuid.NewString()
				if err := f.store.Put(context.Background(), other, []byte("unrelated synthetic credential")); err != nil {
					t.Fatal(err)
				}
				f.store.failDelete = mode == "cleanup_failure"
				s := &promptingStore{memoryStore: f.store, started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{}), mode: mode}
				f.manager.secrets = s
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				a, err := f.manager.Begin(ctx, "", f.open)
				if err != nil {
					t.Fatal(err)
				}
				f.mu.Lock()
				target, state := f.auth.Get("redirect_uri"), f.auth.Get("state")
				f.mu.Unlock()
				callbackDone := make(chan struct{})
				go func() {
					resp, err := http.Get(target + "?" + url.Values{"state": {state}, "code": {"synthetic code"}, "client_id": {"oaiapp_prompt"}}.Encode())
					if err == nil {
						resp.Body.Close()
					}
					close(callbackDone)
				}()
				// Never let a failed regression hang test cleanup behind the prompt.
				var release sync.Once
				t.Cleanup(func() { cancel(); release.Do(func() { close(s.release) }) })
				waitPromptSignal(t, s.started, "credential prompt")
				actionDone := make(chan struct{})
				go func() {
					switch action {
					case "cancel":
						_ = f.manager.Cancel(a.ID)
					case "close":
						_ = f.manager.Close()
					default:
						cancel()
					}
					close(actionDone)
				}()
				waitPromptSignal(t, s.cancelled, "prompt cancellation context")
				release.Do(func() { close(s.release) })
				waitPromptSignal(t, actionDone, "cancel/close")
				waitPromptSignal(t, callbackDone, "sign-in callback")
				got, err := f.manager.Attempt(a.ID)
				if err != nil || got.State != "cancelled" || got.RegistrationID == "" {
					t.Fatal("cancelled attempt published", got, err)
				}
				f.manager.mu.Lock()
				reg := *f.manager.find(got.RegistrationID)
				f.manager.mu.Unlock()
				expected := "signed_out"
				if mode == "cleanup_failure" {
					expected = "sign_out_incomplete"
				}
				if reg.State != expected || reg.PlanAuthorized || !reg.ExpiresAt.IsZero() {
					t.Fatal("cancelled account remained usable", reg)
				}
				if _, err := f.manager.AccessToken(context.Background(), reg.ID); err == nil {
					t.Fatal("cancelled account yielded an access token")
				}
				_, err = f.store.Get(context.Background(), reg.ID)
				if mode != "cleanup_failure" && !errors.Is(err, ErrSecretMissing) {
					t.Fatal("cancelled write retained credentials", err)
				}
				s.mu.Lock()
				if len(s.deleted) != 1 || s.deleted[0] != reg.ID || !s.cleanupContextValid {
					t.Error("cleanup was not isolated and bounded", s.deleted, s.cleanupContextValid)
				}
				s.mu.Unlock()
				if b, err := f.store.Get(context.Background(), other); err != nil || string(b) != "unrelated synthetic credential" {
					t.Fatal("cleanup touched another account", err)
				}
				// The disabled/incomplete registration must survive restart too.
				if err = f.manager.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := newManager(f.root, f.store, roundTrip(f.transport), time.Now)
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				if reopened.find(reg.ID).State != expected {
					t.Fatal("cancellation was not persisted")
				}
				if _, err = reopened.AccessToken(context.Background(), reg.ID); err == nil {
					t.Fatal("restart re-enabled cancelled account")
				}
			})
		}
	}
}

func TestSIWCCancelledCredentialCleanupSerializesNextSignIn(t *testing.T) {
	f := newFixture(t)
	s := &promptingStore{memoryStore: f.store, started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{}), mode: "late_store"}
	f.manager.secrets = s
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := f.manager.Begin(ctx, "", f.open)
	if err != nil {
		t.Fatal(err)
	}
	var release sync.Once
	t.Cleanup(func() { cancel(); release.Do(func() { close(s.release) }) })
	callbackDone := make(chan struct{})
	go func() { f.callback(a, "oaiapp_prompt", nil); close(callbackDone) }()
	waitPromptSignal(t, s.started, "credential prompt")
	s.mu.Lock()
	registration := s.writing
	s.mu.Unlock()
	cancelDone := make(chan struct{})
	go func() { _ = f.manager.Cancel(a.ID); close(cancelDone) }()
	waitPromptSignal(t, s.cancelled, "prompt cancellation")
	type result struct {
		attempt Attempt
		err     error
	}
	nextResult := make(chan result, 1)
	entered := make(chan struct{})
	go func() {
		close(entered)
		next, err := f.manager.Begin(context.Background(), registration, f.open)
		nextResult <- result{next, err}
	}()
	<-entered
	select {
	case <-nextResult:
		t.Fatal("next sign-in overtook the canceled native write and cleanup")
	case <-time.After(100 * time.Millisecond):
	}
	release.Do(func() { close(s.release) })
	waitPromptSignal(t, callbackDone, "cancelled callback")
	waitPromptSignal(t, cancelDone, "cancel")
	var next result
	select {
	case next = <-nextResult:
	case <-time.After(2 * time.Second):
		t.Fatal("next sign-in remained blocked")
	}
	if next.err != nil {
		t.Fatal(next.err)
	}
	f.manager.mu.Lock()
	f.manager.secrets = f.store
	f.manager.mu.Unlock()
	f.callback(next.attempt, "", nil)
	completed, err := f.manager.Attempt(next.attempt.ID)
	if err != nil || completed.State != "completed" || completed.RegistrationID != registration {
		t.Fatal(completed, err)
	}
	if _, err = f.manager.AccessToken(context.Background(), registration); err != nil {
		t.Fatal("cleanup removed the later sign-in's credential", err)
	}
	// An already committed attempt is outside Cancel's scope.
	if err = f.manager.Cancel(next.attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.manager.AccessToken(context.Background(), registration); err != nil {
		t.Fatal("cancelling a completed attempt signed the account out", err)
	}
}
