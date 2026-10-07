package processdomain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// No cgroup is selected implicitly. Once the operator explicitly opts in,
// missing controllers/kernel support/permissions fail, rather than skip.
func nativeConfig(t *testing.T) Config {
	t.Helper()
	root := os.Getenv("TROVE_LINUX_DOMAIN_TEST_ROOT")
	if root == "" {
		t.Skip("PENDING: set TROVE_LINUX_DOMAIN_TEST_ROOT to an owned empty delegated cgroup-v2 domain with cpu,memory,pids enabled")
	}
	limits := testLimits()
	limits.MemoryBytes = 128 << 20
	limits.Processes = 128
	limits.WallTime = 5 * time.Second
	limits.DrainTimeout = 2 * time.Second
	return Config{DelegationRoot: root, Purpose: TrustedQualification, Limits: limits}
}
func nativeDomain(t *testing.T, config Config) *domain {
	t.Helper()
	got, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	d := got.(*domain)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := d.Close(ctx); err != nil {
			t.Errorf("owned fixture cleanup failed: %v", err)
		}
	})
	return d
}
func nativeCommand(t *testing.T, mode, records string) Command {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Command{Executable: executable, Args: []string{"-test.run=^TestLinuxDomainHelper$"}, Directory: filepath.Dir(records), Environment: []string{"TROVE_DOMAIN_HELPER=" + mode, "TROVE_DOMAIN_RECORDS=" + records, "GOMAXPROCS=2", "GOGC=50"}}
}

type helperRecord struct {
	PID       int
	Group     string
	Mode      string
	Migration string
}

func appendRecord(mode, migration string) {
	body, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		os.Exit(21)
	}
	line, err := json.Marshal(helperRecord{PID: os.Getpid(), Group: strings.TrimSpace(string(body)), Mode: mode, Migration: migration})
	if err != nil {
		os.Exit(22)
	}
	out, err := os.OpenFile(os.Getenv("TROVE_DOMAIN_RECORDS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(23)
	}
	_, err = out.Write(append(line, '\n'))
	out.Close()
	if err != nil {
		os.Exit(24)
	}
}
func startHelper(mode string, setsid bool) (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, "-test.run=^TestLinuxDomainHelper$")
	cmd.Env = []string{"TROVE_DOMAIN_HELPER=" + mode, "TROVE_DOMAIN_RECORDS=" + os.Getenv("TROVE_DOMAIN_RECORDS"), "GOMAXPROCS=2", "GOGC=50"}
	if setsid {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	err = cmd.Start()
	return cmd, err
}
func TestLinuxDomainHelper(t *testing.T) {
	mode := os.Getenv("TROVE_DOMAIN_HELPER")
	if mode == "" {
		return
	}
	appendRecord(mode, "")
	switch mode {
	case "tree":
		if _, err := startHelper("session", true); err != nil {
			os.Exit(25)
		}
		time.Sleep(20 * time.Second)
	case "session":
		if _, err := startHelper("grandchild", false); err != nil {
			os.Exit(26)
		}
		time.Sleep(20 * time.Second)
	case "grandchild", "sleep":
		time.Sleep(20 * time.Second)
	case "orphan":
		if _, err := startHelper("sleep", true); err != nil {
			os.Exit(27)
		}
		// Wait for the child to report membership so the assertion is independent
		// of scheduler timing before the leader exits successfully.
		until := time.Now().Add(time.Second)
		for time.Now().Before(until) {
			body, _ := os.ReadFile(os.Getenv("TROVE_DOMAIN_RECORDS"))
			if strings.Count(string(body), "\n") >= 2 {
				os.Exit(0)
			}
			time.Sleep(5 * time.Millisecond)
		}
		os.Exit(28)
	case "pids":
		for i := 0; i < 24; i++ {
			if _, err := startHelper("sleep", false); err != nil {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	case "memory":
		if _, err := startHelper("allocate", false); err != nil {
			os.Exit(29)
		}
		fallthrough
	case "allocate":
		// Each process retains 48 MiB; combined demand exceeds the test's 64 MiB
		// domain bound. No individual allocation alone reaches that bound.
		values := make([]byte, 48<<20)
		for i := 0; i < len(values); i += 4096 {
			values[i] = 1
		}
		time.Sleep(3 * time.Second)
		runtime.KeepAlive(values)
	case "cpu":
		until := time.Now().Add(500 * time.Millisecond)
		var value uint64
		for time.Now().Before(until) {
			for i := 0; i < 10000; i++ {
				value = value*1664525 + 1013904223
			}
		}
		runtime.KeepAlive(value)
	case "migrate":
		target := os.Getenv("TROVE_DOMAIN_ESCAPE_TARGET")
		err := os.WriteFile(target, []byte(strconv.Itoa(os.Getpid())), 0600)
		result := "denied"
		if err == nil {
			result = "allowed"
		}
		appendRecord(mode, result)
		time.Sleep(20 * time.Second)
	default:
		os.Exit(30)
	}
	os.Exit(0)
}
func readRecords(path string) []helperRecord {
	raw, _ := os.ReadFile(path)
	var records []helperRecord
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var r helperRecord
		if json.Unmarshal([]byte(line), &r) == nil {
			records = append(records, r)
		}
	}
	return records
}
func awaitRecords(t *testing.T, path string, count int) []helperRecord {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		records := readRecords(path)
		if len(records) >= count {
			return records
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d independent helper records, got %v", count, readRecords(path))
	return nil
}
func assertNoLiveFixture(t *testing.T, records []helperRecord) {
	t.Helper()
	for _, r := range records {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", r.PID))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		end := strings.LastIndex(string(raw), ")")
		if end < 0 || len(raw) <= end+2 {
			t.Fatal("invalid proc fixture state")
		}
		if raw[end+2] != 'Z' && raw[end+2] != 'X' {
			t.Fatalf("fixture %d remains live after drain", r.PID)
		}
	}
}
func TestLinuxNativeChildGrandchildSessionEscapeAndCancel(t *testing.T) {
	d := nativeDomain(t, nativeConfig(t))
	records := filepath.Join(t.TempDir(), "records.jsonl")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		r   Report
		err error
	}
	done := make(chan outcome, 1)
	command := nativeCommand(t, "tree", records)
	go func() { r, err := d.Execute(ctx, command); done <- outcome{r, err} }()
	got := awaitRecords(t, records, 3)
	expected := d.backend.(*cgroup).name
	for _, r := range got {
		if !strings.Contains(r.Group, "/"+expected) {
			t.Fatalf("descendant escaped cgroup through setsid: %+v", r)
		}
	}
	cancel()
	result := <-done
	if !errors.Is(result.err, context.Canceled) || !result.r.DrainVerified || !result.r.LeaderReaped || !result.r.Removed {
		t.Fatalf("cancel/drain failed: %+v %v", result.r, result.err)
	}
	assertNoLiveFixture(t, got)
}
func TestLinuxNativeSuccessfulParentCannotLeaveOrphan(t *testing.T) {
	d := nativeDomain(t, nativeConfig(t))
	path := filepath.Join(t.TempDir(), "records.jsonl")
	r, err := d.Execute(context.Background(), nativeCommand(t, "orphan", path))
	if err != nil || !r.DrainVerified || !r.Removed {
		t.Fatalf("orphan drain: %+v %v", r, err)
	}
	records := readRecords(path)
	if len(records) != 2 {
		t.Fatalf("missing child evidence: %+v", records)
	}
	assertNoLiveFixture(t, records)
}
func TestLinuxNativeAggregateLimits(t *testing.T) {
	for _, mode := range []string{"pids", "memory", "cpu"} {
		t.Run(mode, func(t *testing.T) {
			config := nativeConfig(t)
			if mode == "pids" {
				config.Limits.Processes = 24
			}
			if mode == "memory" {
				config.Limits.MemoryBytes = 64 << 20
			}
			if mode == "cpu" {
				config.Limits.CPUQuotaMicros = 10000
			}
			d := nativeDomain(t, config)
			path := filepath.Join(t.TempDir(), "records.jsonl")
			report, err := d.Execute(context.Background(), nativeCommand(t, mode, path))
			if !report.DrainVerified || !report.Removed {
				t.Fatalf("limit drain failed: %+v %v", report, err)
			}
			switch mode {
			case "pids":
				if report.Usage.PIDLimitEvents == 0 {
					t.Fatalf("no kernel PID enforcement: %+v %v", report, err)
				}
			case "memory":
				if report.Usage.OOMKills == 0 || err == nil {
					t.Fatalf("no aggregate kernel OOM enforcement: %+v %v", report, err)
				}
			case "cpu":
				if report.Usage.CPUThrottledPeriods == 0 || err != nil {
					t.Fatalf("no kernel CPU throttling: %+v %v", report, err)
				}
			}
			assertNoLiveFixture(t, readRecords(path))
		})
	}
}
func TestLinuxNativeMigrationIsNotProviderContainment(t *testing.T) {
	config := nativeConfig(t)
	source := nativeDomain(t, config)
	destination := nativeDomain(t, config)
	path := filepath.Join(t.TempDir(), "records.jsonl")
	command := nativeCommand(t, "migrate", path)
	command.Environment = append(command.Environment, "TROVE_DOMAIN_ESCAPE_TARGET="+filepath.Join(config.DelegationRoot, destination.backend.(*cgroup).name, "cgroup.procs"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Report, 1)
	go func() { r, _ := source.Execute(ctx, command); done <- r }()
	records := awaitRecords(t, path, 2)
	result := records[len(records)-1].Migration
	if result != "allowed" && result != "denied" {
		t.Fatal("missing migration boundary evidence")
	}
	t.Logf("same-UID migration on this explicit delegation: %s; neither outcome qualifies a provider sandbox", result)
	// The separate destination remains owned too, so even an intentionally moved
	// fixture is bounded and killed; no escaped fixture is left on the host.
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelCleanup()
	if err := destination.Close(cleanup); err != nil {
		t.Fatal(err)
	}
	cancel()
	report := <-done
	if report.ContainmentQualified {
		t.Fatal("accounting was promoted to security containment")
	}
	assertNoLiveFixture(t, records)
	for _, purpose := range []Purpose{Document, Provider} {
		config.Purpose = purpose
		if _, err := New(config); !errors.Is(err, ErrIsolation) {
			t.Fatalf("security request admitted: %v", err)
		}
	}
}
func TestLinuxRejectsOrdinaryFilesystemAsDelegation(t *testing.T) {
	config := Config{DelegationRoot: t.TempDir(), Purpose: TrustedQualification, Limits: testLimits()}
	if _, err := New(config); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("regular directory accepted: %v", err)
	}
	entries, err := os.ReadDir(config.DelegationRoot)
	if err != nil || len(entries) != 0 {
		t.Fatal("ordinary filesystem mutated")
	}
}

func TestLinuxAtomicAdmissionCannotFallBack(t *testing.T) {
	records := filepath.Join(t.TempDir(), "records.jsonl")
	b := &cgroup{dir: -1}
	child, err := b.start(nativeCommand(t, "sleep", records))
	if child != nil {
		cmd := child.(*exec.Cmd)
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal("invalid cgroup FD started unowned code")
	}
	if err == nil {
		t.Fatal("invalid cgroup admission returned success")
	}
	if _, err := os.Stat(records); !os.IsNotExist(err) {
		t.Fatal("user code ran before domain admission")
	}
}
