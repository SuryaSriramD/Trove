// Package processdomain owns a bounded process subtree, not a security sandbox.
// The initial Linux backend admits trusted qualification fixtures only. PDF and
// provider inputs need a separately implemented and qualified isolation layer.
package processdomain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnavailable = errors.New("process domain unavailable")
	ErrIsolation   = errors.New("required process isolation is not implemented or qualified")
	ErrDrain       = errors.New("process domain drain could not be verified")
	ErrUsed        = errors.New("process domain is one-shot and already used or closed")
	ErrWallLimit   = errors.New("process domain exceeded its wall-clock limit")
)

type Purpose string

const (
	TrustedQualification Purpose = "trusted-qualification-fixture"
	Document             Purpose = "document"
	Provider             Purpose = "provider"
)

type Limits struct {
	MemoryBytes     int64
	Processes       int64 // Includes threads: this is the cgroup pids controller limit.
	CPUQuotaMicros  int64
	CPUPeriodMicros int64
	WallTime        time.Duration
	DrainTimeout    time.Duration
}

func (l Limits) validate() error {
	if l.MemoryBytes < 16<<20 || l.MemoryBytes > 1<<40 || l.Processes < 1 || l.Processes > 4096 || l.CPUPeriodMicros < 1000 || l.CPUPeriodMicros > 1000000 || l.CPUQuotaMicros < 1000 || l.CPUQuotaMicros > 64*l.CPUPeriodMicros || l.WallTime <= 0 || l.WallTime > time.Hour || l.DrainTimeout <= 0 || l.DrainTimeout > 30*time.Second {
		return errors.New("explicit bounded memory, PID, CPU, wall and drain limits are required")
	}
	return nil
}

type Config struct {
	DelegationRoot string // Explicit existing delegation; never auto-enables controllers.
	Purpose        Purpose
	Limits         Limits
}

func (c Config) validate() error {
	if c.Purpose != TrustedQualification {
		return ErrIsolation
	}
	if !filepath.IsAbs(c.DelegationRoot) || filepath.Clean(c.DelegationRoot) != c.DelegationRoot {
		return errors.New("an absolute clean delegated cgroup path is required")
	}
	return c.Limits.validate()
}

// Command contains host-selected values only. No ambient environment, implicit shell,
// executable lookup, arbitrary inherited descriptors or caller SysProcAttr are
// accepted. File stdio avoids unbounded io.Copy goroutines during Wait.
type Command struct {
	Executable            string
	Args                  []string
	Directory             string
	Environment           []string
	Stdin, Stdout, Stderr *os.File
}

func (c Command) validate() error {
	if !filepath.IsAbs(c.Executable) || !filepath.IsAbs(c.Directory) || len(c.Args) > 256 || len(c.Environment) > 256 {
		return errors.New("absolute executable/directory and bounded arguments/environment are required")
	}
	size := 0
	seen := map[string]bool{}
	for _, v := range append(append([]string{c.Executable, c.Directory}, c.Args...), c.Environment...) {
		size += len(v)
		if strings.ContainsRune(v, 0) || size > 128<<10 {
			return errors.New("invalid or oversized process arguments/environment")
		}
	}
	for _, v := range c.Environment {
		key, _, ok := strings.Cut(v, "=")
		if !ok || key == "" || seen[key] {
			return errors.New("environment keys must be explicit and unique")
		}
		seen[key] = true
	}
	return nil
}

type Usage struct {
	MemoryCurrentBytes  uint64 `json:"memoryCurrentBytes"`
	MemoryPeakBytes     uint64 `json:"memoryPeakBytes"`
	ProcessesCurrent    uint64 `json:"processesCurrent"`
	CPUUsageMicros      uint64 `json:"cpuUsageMicros"`
	CPUThrottledPeriods uint64 `json:"cpuThrottledPeriods"`
	OOMKills            uint64 `json:"oomKills"`
	PIDLimitEvents      uint64 `json:"pidLimitEvents"`
}
type Report struct {
	Scope                Purpose `json:"scope"`
	Launched             bool    `json:"launched"`
	LeaderReaped         bool    `json:"leaderReaped"`
	DrainVerified        bool    `json:"drainVerified"`
	Removed              bool    `json:"removed"`
	Usage                Usage   `json:"usage"`
	ContainmentQualified bool    `json:"containmentQualified"` // Always false in this foundation.
}

type ProcessDomain interface {
	Execute(context.Context, Command) (Report, error)
	// Close cancels an active Execute or disposes an unused empty domain. A
	// deadline/cancellation returns an error, never a successful drain claim.
	Close(context.Context) error
}

type child interface{ Wait() error }
type backend interface {
	start(Command) (child, error)
	kill() error
	populated() (bool, error)
	usage() (Usage, error)
	release() error
}
type domain struct {
	mu        sync.Mutex
	used      bool
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
	reaped    chan struct{}
	removed   bool
	cleanupMu sync.Mutex
	limits    Limits
	backend   backend
}

func newDomain(b backend, l Limits) *domain {
	return &domain{backend: b, limits: l, stop: make(chan struct{}), done: make(chan struct{})}
}
func (d *domain) finish(removed bool) {
	d.mu.Lock()
	d.removed = removed
	close(d.done)
	d.mu.Unlock()
}
func (d *domain) Execute(ctx context.Context, command Command) (report Report, result error) {
	report.Scope = TrustedQualification
	// Even a rejected command consumes the domain and must release its resources.
	d.mu.Lock()
	if d.used {
		d.mu.Unlock()
		return report, ErrUsed
	}
	d.used = true
	d.mu.Unlock()
	defer func() { d.finish(report.Removed) }()
	if err := command.validate(); err != nil {
		return report, errors.Join(err, d.dispose(&report))
	}
	if err := ctx.Err(); err != nil {
		return report, errors.Join(err, d.dispose(&report))
	}
	select {
	case <-d.stop:
		return report, errors.Join(context.Canceled, d.dispose(&report))
	default:
	}
	process, err := d.backend.start(command)
	if err != nil {
		return report, errors.Join(fmt.Errorf("atomic process-domain admission failed: %w", err), d.dispose(&report))
	}
	report.Launched = true
	exited := make(chan error, 1)
	reaped := make(chan struct{})
	d.mu.Lock()
	d.reaped = reaped
	d.mu.Unlock()
	go func() { exited <- process.Wait(); close(reaped) }()
	timer := time.NewTimer(d.limits.WallTime)
	defer timer.Stop()
	var cause error
	select {
	case cause = <-exited:
		report.LeaderReaped = true
	case <-ctx.Done():
		cause = ctx.Err()
	case <-d.stop:
		cause = context.Canceled
	case <-timer.C:
		cause = ErrWallLimit
	}
	// Always kill the domain after leader exit too: a successful parent can leave
	// a detached child behind. Poll the recursive populated flag, not a PID list.
	cleanupCtx, cancel := context.WithTimeout(context.Background(), d.limits.DrainTimeout)
	defer cancel()
	drainErr := d.drain(cleanupCtx)
	if !report.LeaderReaped {
		select {
		case waitErr := <-exited:
			report.LeaderReaped = true
			cause = errors.Join(cause, waitErr)
		case <-cleanupCtx.Done():
			drainErr = errors.Join(drainErr, fmt.Errorf("%w: leader not reaped", ErrDrain))
		}
	}
	report.DrainVerified = drainErr == nil && report.LeaderReaped
	usage, usageErr := d.backend.usage()
	report.Usage = usage
	if report.DrainVerified {
		err = d.backend.release()
		report.Removed = err == nil
	} else {
		err = ErrDrain
	} // Keep handles/path for explicit cleanup retry.
	return report, errors.Join(cause, drainErr, usageErr, err)
}
func (d *domain) drain(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrDrain, err)
	}
	if err := d.backend.kill(); err != nil {
		return fmt.Errorf("%w: kill: %v", ErrDrain, err)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrDrain, err)
		}
		populated, err := d.backend.populated()
		if err != nil {
			return fmt.Errorf("%w: membership unavailable: %v", ErrDrain, err)
		}
		if !populated {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Join(ErrDrain, ctx.Err())
		case <-tick.C:
		}
	}
}
func (d *domain) dispose(report *Report) error {
	ctx, cancel := context.WithTimeout(context.Background(), d.limits.DrainTimeout)
	defer cancel()
	if err := d.drain(ctx); err != nil {
		return err
	}
	report.DrainVerified = true // No child was launched by this domain.
	err := d.backend.release()
	report.Removed = err == nil
	return err
}
func (d *domain) Close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, d.limits.DrainTimeout)
	defer cancel()
	d.mu.Lock()
	if !d.used {
		d.used = true
		d.mu.Unlock()
		report := Report{}
		err := d.drain(ctx)
		if err == nil {
			err = d.backend.release()
			report.Removed = err == nil
		}
		d.finish(report.Removed)
		return err
	}
	d.mu.Unlock()
	d.stopOnce.Do(func() { close(d.stop) })
	select {
	case <-d.done:
		d.cleanupMu.Lock()
		defer d.cleanupMu.Unlock()
		d.mu.Lock()
		removed := d.removed
		d.mu.Unlock()
		if removed {
			return nil
		}
		if err := d.drain(ctx); err != nil {
			return err
		}
		d.mu.Lock()
		reaped := d.reaped
		d.mu.Unlock()
		if reaped != nil {
			select {
			case <-reaped:
			case <-ctx.Done():
				return errors.Join(ErrDrain, ctx.Err())
			}
		}
		err := d.backend.release()
		d.mu.Lock()
		d.removed = err == nil
		d.mu.Unlock()
		return err
	case <-ctx.Done():
		return errors.Join(ErrDrain, ctx.Err())
	}
}
