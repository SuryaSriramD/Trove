package native

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"trove.local/phase0/contracts"
)

type rejectedNetwork struct{ calls int }

func (r *rejectedNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls++
	return nil, errors.New("unexpected network access")
}

func TestMissingModelNeverAcquiresOrFallsBack(t *testing.T) {
	m, _ := NewManager(t.TempDir())
	network := &rejectedNetwork{}
	m.client.Transport = network
	s := &Supervisor{manager: m}
	if _, err := s.GenerateText(context.Background(), NewID(), "Local question", 32, nil); err == nil || !strings.Contains(err.Error(), "MODEL_NOT_INSTALLED") {
		t.Fatalf("missing instruction model did not give explicit failure: %v", err)
	}
	for _, operation := range []string{"embed", "transcribe"} {
		if _, err := s.Run(context.Background(), Request{Wire: contracts.JobsWorkerRequest{ModelPackId: contracts.DomainId(NewID()), Operation: operation}}, nil); err == nil {
			t.Fatalf("missing %s pack unexpectedly succeeded", operation)
		}
	}
	if network.calls != 0 {
		t.Fatal("ordinary inference attempted network acquisition")
	}
	files, _ := os.ReadDir(m.root)
	if len(files) != 0 {
		t.Fatal("inference created model acquisition state")
	}
}

func TestModelDownloadRejectsHTTPSDowngrade(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/model", http.StatusFound)
	}))
	defer server.Close()
	m, _ := NewManager(t.TempDir())
	// Retain the production redirect policy while trusting the local TLS fixture.
	m.client.Transport = server.Client().Transport
	p := fixturePack([]byte("GGUFfixture"))
	p.URL = server.URL
	if err := m.Acquire(context.Background(), p, nil); err == nil || !strings.Contains(err.Error(), "invalid model redirect") {
		t.Fatalf("insecure redirect accepted: %v", err)
	}
}

func TestAcquisitionRejectsIncompatiblePacksBeforeIO(t *testing.T) {
	data := []byte("GGUFfixture")
	for name, change := range map[string]func(*Pack){
		"architecture": func(p *Pack) { p.Architecture = "amd64" },
		"runtime":      func(p *Pack) { p.RuntimeCommit = "unqualified-runtime" },
		"format":       func(p *Pack) { p.Format = "pickle" },
		"identity":     func(p *Pack) { p.ID = "../../outside" },
		"hash":         func(p *Pack) { p.SHA256 = "missing" },
		"memory":       func(p *Pack) { p.EstimatedMemoryBytes = 1 },
		"kind":         func(p *Pack) { p.Kind = "remote" },
	} {
		t.Run(name, func(t *testing.T) {
			m, err := NewManager(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			p := fixturePack(data)
			change(&p)
			for _, acquire := range []func() error{
				func() error { return m.Import(context.Background(), p, bytes.NewReader(data), nil) },
				func() error { return m.Acquire(context.Background(), p, nil) },
			} {
				if err = acquire(); err == nil || !strings.Contains(err.Error(), "MODEL_INCOMPATIBLE") {
					t.Fatalf("incompatible pack was not rejected: %v", err)
				}
			}
			files, _ := os.ReadDir(m.root)
			if len(files) != 0 {
				t.Fatal("incompatible pack wrote model files")
			}
		})
	}
}

func TestAcquisitionInvalidHashIsVisibleAndRetryRecovers(t *testing.T) {
	data := []byte("GGUFvalid model fixture")
	var attempts atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "\"fixture\"")
		if attempts.Add(1) == 1 {
			bad := bytes.Clone(data)
			bad[len(bad)-1] ^= 1
			_, _ = w.Write(bad)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	p := fixturePack(data)
	p.URL = server.URL
	m, _ := NewManager(t.TempDir())
	m.client = server.Client()
	if err := m.Acquire(context.Background(), p, nil); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatal(err)
	}
	if _, _, _, err := m.lease(p.ID); err == nil {
		t.Fatal("invalid model was installed")
	}
	list, err := m.List()
	if err != nil || len(list) != 1 || list[0].State != "corrupt" {
		t.Fatalf("missing visible corruption: %+v %v", list, err)
	}
	// New process must recover without manually removing cache files.
	m, _ = NewManager(m.root)
	m.client = server.Client()
	if err = m.Acquire(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatal("retry did not reacquire corrupted bytes")
	}
	list, err = m.List()
	if err != nil || len(list) != 1 || list[0].State != "ready" {
		t.Fatalf("retry failed: %+v %v", list, err)
	}
}

func TestImportInvalidMagicAndInterruptionAreRecoverable(t *testing.T) {
	m, _ := NewManager(t.TempDir())
	bad := []byte("PICKhostile serialized model")
	p := fixturePack(bad)
	if err := m.Import(context.Background(), p, bytes.NewReader(bad), nil); err == nil || !strings.Contains(err.Error(), "model magic") {
		t.Fatal(err)
	}
	data := append([]byte("GGUF"), bytes.Repeat([]byte("x"), 256<<10)...)
	p = fixturePack(data)
	ctx, cancel := context.WithCancel(context.Background())
	err := m.Import(ctx, p, bytes.NewReader(data), func(n int64) {
		if n >= 65536 {
			cancel()
		}
	})
	cancel()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("import interruption: %v", err)
	}
	m, _ = NewManager(m.root)
	list, err := m.List()
	if err != nil || len(list) != 1 || list[0].State != "interrupted" || list[0].DownloadedBytes == 0 {
		t.Fatalf("missing interrupted import: %+v %v", list, err)
	}
	if _, _, _, err = m.lease(p.ID); err == nil {
		t.Fatal("interrupted model could be used")
	}
	if err = m.Import(context.Background(), p, bytes.NewReader(data), nil); err != nil {
		t.Fatal(err)
	}
}

func TestModelLowDiskPreflightPreservesInstalledPack(t *testing.T) {
	data := []byte("GGUFvalid model fixture")
	p := fixturePack(data)
	m, _ := NewManager(t.TempDir())
	if err := m.Import(context.Background(), p, bytes.NewReader(data), nil); err != nil {
		t.Fatal(err)
	}
	m.availableBytes = func(string) (uint64, error) { return uint64(p.SizeBytes + modelDiskReserve - 1), nil }
	if err := m.Import(context.Background(), p, bytes.NewReader(data), nil); err == nil || !strings.Contains(err.Error(), "RESOURCE_EXHAUSTED") {
		t.Fatalf("low disk import was admitted: %v", err)
	}
	_, _, release, err := m.lease(p.ID)
	if err != nil {
		t.Fatalf("low disk import damaged installed pack: %v", err)
	}
	release()
	if err = m.Remove(p.ID); err != nil {
		t.Fatal(err)
	}
	if err = m.Acquire(context.Background(), p, nil); err == nil || !strings.Contains(err.Error(), "RESOURCE_EXHAUSTED") {
		t.Fatalf("low disk download was admitted: %v", err)
	}
	files, _ := os.ReadDir(m.root)
	if len(files) != 0 {
		t.Fatal("rejected download left artifacts")
	}
}

func TestAcquisitionDiskExhaustionRetainsResumableBytes(t *testing.T) {
	data := append([]byte("GGUF"), bytes.Repeat([]byte("x"), 10<<20)...)
	p := fixturePack(data)
	var ranged atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "\"fixture\"")
		start := 0
		if h := r.Header.Get("Range"); h != "" {
			_, _ = fmt.Sscanf(h, "bytes=%d-", &start)
			ranged.Store(true)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(data[start:])
	}))
	defer server.Close()
	p.URL = server.URL
	m, _ := NewManager(t.TempDir())
	m.client = server.Client()
	checks := 0
	m.availableBytes = func(string) (uint64, error) {
		checks++
		if checks >= 4 {
			return uint64(modelDiskReserve), nil
		}
		return 1 << 30, nil
	}
	if err := m.Acquire(context.Background(), p, nil); err == nil || !strings.Contains(err.Error(), "RESOURCE_EXHAUSTED") {
		t.Fatalf("disk exhaustion was ignored: %v", err)
	}
	list, err := m.List()
	if err != nil || len(list) != 1 || list[0].State != "interrupted" || list[0].DownloadedBytes == 0 || list[0].DownloadedBytes >= p.SizeBytes {
		t.Fatalf("partial transfer lost: %+v %v", list, err)
	}
	m, _ = NewManager(m.root)
	m.client = server.Client()
	if err = m.Acquire(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if !ranged.Load() {
		t.Fatal("disk recovery did not resume")
	}
}

func TestAcquisitionRangeMismatchRetainsPartial(t *testing.T) {
	data := []byte("GGUFvalid model fixture")
	p := fixturePack(data)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(data)-1, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data)
	}))
	defer server.Close()
	p.URL = server.URL
	m, _ := NewManager(t.TempDir())
	m.client = server.Client()
	_, part, _ := m.paths(p)
	if err := os.WriteFile(part, data[:4], 0600); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(part+".json", acquisition{Pack: p, ETag: "\"fixture\"", State: "acquiring"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Acquire(context.Background(), p, nil); err == nil || !strings.Contains(err.Error(), "invalid model range") {
		t.Fatal(err)
	}
	partial, err := os.ReadFile(part)
	if err != nil || !bytes.Equal(partial, data[:4]) {
		t.Fatal("invalid range damaged partial bytes", err)
	}
}
