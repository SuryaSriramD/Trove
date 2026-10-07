package processdomain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func testLimits() Limits {
	return Limits{MemoryBytes: 64 << 20, Processes: 64, CPUQuotaMicros: 50000, CPUPeriodMicros: 100000, WallTime: time.Second, DrainTimeout: 50 * time.Millisecond}
}
func testCommand() Command {
	return Command{Executable: filepath.Join(string(os.PathSeparator), "fixture"), Directory: string(os.PathSeparator), Environment: []string{"PATH=/nonexistent"}}
}

type fixtureBackend struct {
	mu                                  sync.Mutex
	started                             int
	kills                               int
	releases                            int
	live                                bool
	keepLive                            bool
	startErr, killErr, popErr, usageErr error
	exited                              chan error
	ready                               chan struct{}
	killOnce                            sync.Once
}

func fixture() *fixtureBackend {
	return &fixtureBackend{live: true, exited: make(chan error, 1), ready: make(chan struct{})}
}
func (f *fixtureBackend) start(Command) (child, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started++
	close(f.ready)
	if f.startErr != nil {
		return nil, f.startErr
	}
	return f, nil
}
func (f *fixtureBackend) Wait() error { return <-f.exited }
func (f *fixtureBackend) kill() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kills++
	if f.killErr != nil {
		return f.killErr
	}
	if !f.keepLive {
		f.live = false
	}
	f.killOnce.Do(func() {
		select {
		case f.exited <- nil:
		default:
		}
	})
	return nil
}
func (f *fixtureBackend) populated() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live, f.popErr
}
func (f *fixtureBackend) usage() (Usage, error) {
	return Usage{MemoryPeakBytes: 123, CPUUsageMicros: 45}, f.usageErr
}
func (f *fixtureBackend) release() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.live {
		return errors.New("independent fixture: release while descendants alive")
	}
	f.releases++
	return nil
}
func (f *fixtureBackend) counts() (int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started, f.kills, f.releases
}

func TestAdmissionRejectsSecurityClaimsAndUnboundedLimits(t *testing.T) {
	for _, purpose := range []Purpose{Document, Provider, "", Purpose("arbitrary")} {
		if _, err := New(Config{DelegationRoot: "/must-not-open", Purpose: purpose, Limits: testLimits()}); !errors.Is(err, ErrIsolation) {
			t.Fatalf("%s admitted: %v", purpose, err)
		}
	}
	for _, mutate := range []func(*Limits){func(l *Limits) { l.MemoryBytes = 0 }, func(l *Limits) { l.Processes = 0 }, func(l *Limits) { l.CPUQuotaMicros = 0 }, func(l *Limits) { l.WallTime = 0 }, func(l *Limits) { l.DrainTimeout = 0 }} {
		limits := testLimits()
		mutate(&limits)
		if err := (Config{DelegationRoot: "/must-not-open", Purpose: TrustedQualification, Limits: limits}).validate(); err == nil {
			t.Fatal("unbounded resource policy admitted")
		}
	}
}
func TestNativeLinuxQualificationPendingOnOtherHosts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("PENDING: Linux cgroup2 child/grandchild, session escape, limits, cancel and drain require an explicit native delegated hierarchy")
	}
}
func TestCanceledOrInvalidCommandNeverStarts(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		b := fixture()
		d := newDomain(b, testLimits())
		ctx, cancel := context.WithCancel(context.Background())
		command := testCommand()
		if invalid {
			command.Environment = []string{"A=1", "A=2"}
		} else {
			cancel()
		}
		r, err := d.Execute(ctx, command)
		cancel()
		started, _, removed := b.counts()
		if err == nil || r.Launched || started != 0 || removed != 1 || !r.Removed {
			t.Fatalf("invalid launch: %+v %v", r, err)
		}
	}
}
func TestAtomicAdmissionFailureCannotBecomeSuccessfulNoop(t *testing.T) {
	b := fixture()
	b.startErr = errors.New("clone3 refused")
	d := newDomain(b, testLimits())
	r, err := d.Execute(context.Background(), testCommand())
	if err == nil || !strings.Contains(err.Error(), "admission failed") || r.Launched || r.ContainmentQualified || !r.Removed {
		t.Fatalf("false launch evidence: %+v %v", r, err)
	}
}
func TestLeaderSuccessStillKillsAndDrainsDetachedDescendants(t *testing.T) {
	b := fixture()
	b.exited <- nil
	d := newDomain(b, testLimits())
	r, err := d.Execute(context.Background(), testCommand())
	_, kills, removed := b.counts()
	if err != nil || kills != 1 || removed != 1 || !r.LeaderReaped || !r.DrainVerified || !r.Removed || r.ContainmentQualified || r.Usage.MemoryPeakBytes != 123 {
		t.Fatalf("parent exit lost descendants: %+v %v", r, err)
	}
	if _, err = d.Execute(context.Background(), testCommand()); !errors.Is(err, ErrUsed) {
		t.Fatal("domain reused")
	}
}
func TestCancellationAndWallLimitBothDrain(t *testing.T) {
	for _, wall := range []bool{false, true} {
		t.Run(map[bool]string{true: "wall", false: "cancel"}[wall], func(t *testing.T) {
			b := fixture()
			limits := testLimits()
			limits.WallTime = 20 * time.Millisecond
			d := newDomain(b, limits)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if !wall {
				go func() { <-b.ready; cancel() }()
			}
			r, err := d.Execute(ctx, testCommand())
			expected := error(context.Canceled)
			if wall {
				expected = ErrWallLimit
			}
			if !errors.Is(err, expected) || !r.DrainVerified || !r.LeaderReaped || !r.Removed {
				t.Fatalf("unverified cancel: %+v %v", r, err)
			}
		})
	}
}
func TestUnverifiableDrainNeverRemovesOrPasses(t *testing.T) {
	for _, unreadable := range []bool{false, true} {
		t.Run(map[bool]string{true: "unreadable", false: "populated"}[unreadable], func(t *testing.T) {
			b := fixture()
			b.exited <- nil
			if unreadable {
				b.popErr = errors.New("denied")
			} else {
				b.keepLive = true
			}
			d := newDomain(b, testLimits())
			r, err := d.Execute(context.Background(), testCommand())
			_, _, removed := b.counts()
			if !errors.Is(err, ErrDrain) || r.DrainVerified || r.Removed || removed != 0 {
				t.Fatalf("false drain: %+v %v", r, err)
			}
			b.mu.Lock()
			b.keepLive = false
			b.popErr = nil
			b.mu.Unlock()
			if err = d.Close(context.Background()); err != nil {
				t.Fatalf("owned cleanup cannot retry: %v", err)
			}
		})
	}
}
func TestMissingAggregateUsageMakesExecutionFail(t *testing.T) {
	b := fixture()
	b.exited <- nil
	b.usageErr = errors.New("memory counter denied")
	r, err := newDomain(b, testLimits()).Execute(context.Background(), testCommand())
	if err == nil || !r.DrainVerified || !r.Removed {
		t.Fatalf("missing usage accepted: %+v %v", r, err)
	}
}
func TestCloseCancelsActiveRunAndDisposesUnusedDomain(t *testing.T) {
	b := fixture()
	d := newDomain(b, testLimits())
	done := make(chan error, 1)
	go func() { _, err := d.Execute(context.Background(), testCommand()); done <- err }()
	<-b.ready
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("close did not cancel: %v", err)
	}
	b = fixture()
	d = newDomain(b, testLimits())
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Execute(context.Background(), testCommand()); !errors.Is(err, ErrUsed) {
		t.Fatal("closed domain launched")
	}
}
func TestCountersRejectAmbiguityOverflowAndMissingValues(t *testing.T) {
	for _, raw := range []string{"", "usage_usec -1", "usage_usec 1\nusage_usec 2", "populated 1 extra", "a 18446744073709551616", strings.Repeat("x", (16<<10)+1)} {
		if _, err := parseCounters(raw); err == nil {
			t.Fatalf("ambiguous counter accepted: %q", raw)
		}
	}
	values, err := parseCounters("usage_usec 42\nnr_throttled 7")
	if err != nil || values["usage_usec"] != 42 || values["nr_throttled"] != 7 {
		t.Fatalf("valid counters: %v %v", values, err)
	}
}

func TestFailedKillCannotRemoveLiveDomain(t *testing.T) {
	b := fixture()
	b.exited <- nil
	b.killErr = errors.New("cgroup.kill denied")
	d := newDomain(b, testLimits())
	r, err := d.Execute(context.Background(), testCommand())
	_, _, removed := b.counts()
	if !errors.Is(err, ErrDrain) || r.DrainVerified || removed != 0 {
		t.Fatalf("failed kill accepted: %+v %v", r, err)
	}
	b.mu.Lock()
	b.killErr = nil
	b.mu.Unlock()
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestCloseCannotWaitWithoutConfiguredDeadline(t *testing.T) {
	b := fixture()
	b.keepLive = true
	limits := testLimits()
	limits.DrainTimeout = 20 * time.Millisecond
	d := newDomain(b, limits)
	started := time.Now()
	err := d.Close(context.Background())
	if !errors.Is(err, ErrDrain) || time.Since(started) > time.Second {
		t.Fatalf("unbounded close: %v", err)
	}
	b.mu.Lock()
	b.keepLive = false
	b.mu.Unlock()
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
