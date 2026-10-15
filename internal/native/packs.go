// Package native owns local model acquisition and supervised native computation.
// It has no database access: callers commit validated proposals through workspace services.
package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"trove.local/phase0/internal/platformfs"
)

const LlamaCommit = "d2e54583c7452353eb35d40431281f6ee984332f"
const WhisperCommit = "927cfce34f31707e17f2bff35c349632fb9e2c3a"

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Pack struct {
	SchemaVersion        int             `json:"schemaVersion"`
	Projector            *VisionArtifact `json:"projector,omitempty"`
	VisionVersion        int             `json:"visionVersion,omitempty"`
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	Kind                 string          `json:"kind"`
	Format               string          `json:"format"`
	SHA256               string          `json:"sha256"`
	SizeBytes            int64           `json:"sizeBytes"`
	URL                  string          `json:"url"`
	License              string          `json:"license"`
	LicenseURL           string          `json:"licenseUrl"`
	SourceRevision       string          `json:"sourceRevision"`
	RuntimeCommit        string          `json:"runtimeCommit"`
	Architecture         string          `json:"architecture"`
	ContextTokens        int             `json:"contextTokens"`
	Dimension            int             `json:"dimension,omitempty"`
	EstimatedMemoryBytes int64           `json:"estimatedMemoryBytes"`
	QualificationOnly    bool            `json:"qualificationOnly,omitempty"`
}

type PackStatus struct {
	Pack            Pack   `json:"pack"`
	State           string `json:"state"`
	DownloadedBytes int64  `json:"downloadedBytes"`
}
type Manager struct {
	root   string
	mu     sync.Mutex
	busy   map[string]bool
	leases map[string]int
	owners map[string]*os.File
	client *http.Client
	// Injectable only inside this package so disk exhaustion is reproducible
	// without filling the user's actual model volume during qualification.
	availableBytes func(string) (uint64, error)
}

func NewManager(root string) (*Manager, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &Manager{root: root, busy: map[string]bool{}, leases: map[string]int{}, owners: map[string]*os.File{}, availableBytes: availableModelBytes, client: &http.Client{Timeout: 2 * time.Hour, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) > 5 {
			return errors.New("invalid model redirect")
		}
		return nil
	}}}, nil
}

const modelDiskReserve int64 = 16 << 20

func (m *Manager) requireDisk(bytes int64) error {
	available, err := m.availableBytes(m.root)
	if err != nil {
		return fmt.Errorf("cannot inspect model storage: %w", err)
	}
	if available < uint64(bytes+modelDiskReserve) {
		return fmt.Errorf("RESOURCE_EXHAUSTED: model storage needs %d bytes plus %d bytes free reserve; available %d", bytes, modelDiskReserve, available)
	}
	return nil
}

// Recheck while copying because another application may consume disk after
// admission. A failed write keeps the partial transfer available for retry.
type modelWriter struct {
	manager    *Manager
	file       *os.File
	sinceCheck int64
}

func (w *modelWriter) Write(b []byte) (int, error) {
	if w.sinceCheck == 0 || w.sinceCheck >= 8<<20 {
		if err := w.manager.requireDisk(int64(len(b))); err != nil {
			return 0, err
		}
		w.sinceCheck = 0
	}
	n, err := w.file.Write(b)
	w.sinceCheck += int64(n)
	if isDiskFull(err) {
		err = fmt.Errorf("RESOURCE_EXHAUSTED: model storage is full: %w", err)
	}
	return n, err
}
func (p Pack) Validate() error {
	if (p.SchemaVersion != 1 && p.SchemaVersion != 2) || !uuidPattern.MatchString(p.ID) || !digestPattern.MatchString(p.SHA256) || p.SizeBytes < 4 || p.SizeBytes > 32<<30 || p.Name == "" || len(p.Name) > 256 || p.License == "" || p.LicenseURL == "" || p.SourceRevision == "" {
		return errors.New("MODEL_INCOMPATIBLE: invalid manifest")
	}
	if err := p.validateVisionManifest(); err != nil {
		return err
	}
	if !supportedNativeTarget(runtime.GOOS, runtime.GOARCH) || (p.Architecture != "portable" && p.Architecture != runtime.GOARCH) {
		return errors.New("MODEL_INCOMPATIBLE: architecture")
	}
	if p.EstimatedMemoryBytes < p.SizeBytes || p.EstimatedMemoryBytes > 16<<30 {
		return errors.New("MODEL_INCOMPATIBLE: memory estimate")
	}
	switch p.Kind {
	case "chat", "embedding", "vision":
		if p.Format != "gguf" || p.RuntimeCommit != LlamaCommit || p.ContextTokens < 1 || p.ContextTokens > 131072 {
			return errors.New("MODEL_INCOMPATIBLE: llama pack")
		}
		if p.Kind == "embedding" && (p.Dimension < 1 || p.Dimension > 8192) {
			return errors.New("MODEL_INCOMPATIBLE: embedding dimension")
		}
	case "vision_projector":
		if p.Format != "gguf" || p.RuntimeCommit != LlamaCommit || p.SizeBytes > 512<<20 || p.ContextTokens != 0 {
			return errors.New("MODEL_INCOMPATIBLE: vision projector")
		}
	case "speech":
		if p.Format != "whisper-ggml" || p.RuntimeCommit != WhisperCommit {
			return errors.New("MODEL_INCOMPATIBLE: speech pack")
		}
	default:
		return errors.New("MODEL_INCOMPATIBLE: kind")
	}
	if p.URL != "" {
		u, e := url.Parse(p.URL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return errors.New("invalid model URL")
		}
	}
	return nil
}
func (m *Manager) paths(p Pack) (string, string, string) {
	base := filepath.Join(m.root, p.ID)
	return base + ".model", base + ".part", base + ".json"
}
func atomicJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(path+".tmp", path); e != nil {
		return e
	}
	return syncModelDirectory(filepath.Dir(path))
}
func (m *Manager) begin(p Pack) error {
	if e := p.Validate(); e != nil {
		return e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy[p.ID] || m.leases[p.ID] > 0 {
		return errors.New("RESOURCE_EXHAUSTED: model in use")
	}
	owner, err := m.acquireOwner(p.ID)
	if err != nil {
		return err
	}
	m.owners[p.ID] = owner
	m.busy[p.ID] = true
	return nil
}
func (m *Manager) acquireOwner(id string) (*os.File, error) {
	owner, err := platformfs.AcquireLock(filepath.Join(m.root, id+".owner.lock"))
	if err != nil {
		return nil, fmt.Errorf("RESOURCE_EXHAUSTED: model is owned by another operation or application: %w", err)
	}
	return owner, nil
}
func (m *Manager) end(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.busy, id)
	_ = platformfs.ReleaseLock(m.owners[id])
	delete(m.owners, id)
}

func regular(path string) error {
	s, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !s.Mode().IsRegular() {
		return errors.New("model must be regular file")
	}
	return nil
}
func verifyFile(path string, p Pack) error {
	if e := regular(path); e != nil {
		return e
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil || s.Size() != p.SizeBytes {
		return errors.New("model size mismatch")
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return e
	}
	if hex.EncodeToString(h.Sum(nil)) != p.SHA256 {
		return errors.New("model digest mismatch")
	}
	var magic [4]byte
	if _, e = f.ReadAt(magic[:], 0); e != nil {
		return e
	}
	if (p.Format == "gguf" && string(magic[:]) != "GGUF") || (p.Format == "whisper-ggml" && string(magic[:]) != "lmgg") {
		return errors.New("MODEL_INCOMPATIBLE: model magic")
	}
	return nil
}
func (m *Manager) finish(p Pack) error {
	final, part, meta := m.paths(p)
	if e := verifyFile(part, p); e != nil {
		// A complete transfer with a bad digest must not become a permanent
		// retry loop. Retain its failure status but discard untrusted bytes.
		_ = os.Remove(part)
		_ = atomicJSON(part+".json", acquisition{Pack: p, State: "corrupt"})
		return e
	}
	if e := os.Rename(part, final); e != nil {
		return e
	}
	if e := atomicJSON(meta, p); e != nil {
		return e
	}
	os.Remove(part + ".json")
	return nil
}

type progressReader struct {
	ctx      context.Context
	r        io.Reader
	n        int64
	max      int64
	progress func(int64)
}

func (r *progressReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	if len(b) > 65536 {
		b = b[:65536]
	}
	n, e := r.r.Read(b)
	r.n += int64(n)
	if r.n > r.max {
		return 0, errors.New("model exceeds declared length")
	}
	if r.progress != nil {
		r.progress(r.n)
	}
	return n, e
}

// Import accepts a native-picker opened descriptor, never a renderer-supplied path.
func (m *Manager) Import(ctx context.Context, p Pack, input io.Reader, progress func(int64)) error {
	if e := m.begin(p); e != nil {
		return e
	}
	defer m.end(p.ID)
	if p.Projector != nil {
		return errors.New("INVALID_INPUT: vision import requires the model and its matching projector")
	}
	return m.importContent(ctx, p, input, progress)
}
func (m *Manager) importContent(ctx context.Context, p Pack, input io.Reader, progress func(int64)) error {
	_, part, _ := m.paths(p)
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := m.requireDisk(p.SizeBytes); e != nil {
		return e
	}
	if e := atomicJSON(part+".json", acquisition{Pack: p, State: "importing"}); e != nil {
		return e
	}
	f, e := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(&modelWriter{manager: m, file: f}, &progressReader{ctx: ctx, r: input, max: p.SizeBytes, progress: progress})
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if e != nil {
		return e
	}
	return m.finish(p)
}

type acquisition struct {
	Pack  Pack   `json:"pack"`
	ETag  string `json:"etag"`
	State string `json:"state"`
}

// Acquire persists .part bytes and the manifest/ETag; a new Manager resumes after a crash.
func (m *Manager) Acquire(ctx context.Context, p Pack, progress func(int64)) error {
	if e := m.begin(p); e != nil {
		return e
	}
	defer m.end(p.ID)
	if e := ctx.Err(); e != nil {
		return e
	}
	final, part, _ := m.paths(p)
	if p.Projector != nil {
		var previous acquisition
		raw, _ := os.ReadFile(part + ".json")
		_ = json.Unmarshal(raw, &previous)
		if previous.Pack.SHA256 != p.SHA256 {
			previous.ETag = ""
		}
		previous.Pack = p
		previous.State = "acquiring"
		if err := atomicJSON(part+".json", previous); err != nil {
			return err
		}
		if err := m.Acquire(ctx, p.projectorPack(), progress); err != nil {
			return err
		}
		progress = offsetModelProgress(progress, p.Projector.SizeBytes)
	}
	if verifyFile(final, p) == nil {
		if err := atomicJSON(filepath.Join(m.root, p.ID+".json"), p); err != nil {
			return err
		}
		if p.Projector != nil {
			if err := os.Remove(part + ".json"); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	}
	var a acquisition
	raw, _ := os.ReadFile(part + ".json")
	_ = json.Unmarshal(raw, &a)
	offset := int64(0)
	if a.Pack.SHA256 == p.SHA256 && a.Pack.SizeBytes == p.SizeBytes && a.ETag != "" {
		if s, e := os.Stat(part); e == nil && s.Mode().IsRegular() {
			offset = s.Size()
		}
	}
	if offset == p.SizeBytes {
		if verifyFile(part, p) == nil {
			return m.finish(p)
		}
		offset = 0
	}
	if offset > p.SizeBytes {
		offset = 0
	}
	if e := m.requireDisk(p.SizeBytes - offset); e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", p.URL, nil)
	if e != nil {
		return e
	}
	req.Header.Set("Accept-Encoding", "identity")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		req.Header.Set("If-Range", a.ETag)
	}
	resp, e := m.client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return fmt.Errorf("model HTTP status %d", resp.StatusCode)
	}
	if resp.StatusCode == 206 {
		want := fmt.Sprintf("bytes %d-", offset)
		if !strings.HasPrefix(resp.Header.Get("Content-Range"), want) || !strings.HasSuffix(resp.Header.Get("Content-Range"), "/"+strconv.FormatInt(p.SizeBytes, 10)) {
			return errors.New("invalid model range")
		}
	} else {
		offset = 0
	}
	// A server may ignore Range after changing its entity. Check the full
	// replacement size before discarding a previously durable partial file.
	if e = m.requireDisk(p.SizeBytes - offset); e != nil {
		return e
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	f, e := os.OpenFile(part, flags, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = f.Seek(offset, 0); e != nil {
		return e
	}
	if e = atomicJSON(part+".json", acquisition{p, resp.Header.Get("ETag"), "acquiring"}); e != nil {
		return e
	}
	_, e = io.Copy(&modelWriter{manager: m, file: f}, &progressReader{ctx: ctx, r: resp.Body, n: offset, max: p.SizeBytes, progress: progress})
	if e == nil {
		e = f.Sync()
	}
	if e != nil {
		return e
	}
	f.Close()
	return m.finish(p)
}
func (m *Manager) List() ([]PackStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, e := os.ReadDir(m.root)
	if e != nil {
		return nil, e
	}
	out := []PackStatus{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(m.root, entry.Name()))
		if e != nil {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".part.json") {
			var a acquisition
			if json.Unmarshal(raw, &a) == nil && a.Pack.Validate() == nil && a.Pack.Kind != "vision_projector" {
				s := PackStatus{Pack: a.Pack, State: "interrupted"}
				if a.State == "corrupt" {
					s.State = "corrupt"
				}
				if m.busy[a.Pack.ID] {
					s.State = "acquiring"
				}
				if st, e := os.Stat(filepath.Join(m.root, a.Pack.ID+".part")); e == nil {
					s.DownloadedBytes = st.Size()
				}
				s.DownloadedBytes += m.projectorDownloaded(a.Pack)
				out = append(out, s)
			}
			continue
		}
		var p Pack
		if json.Unmarshal(raw, &p) == nil && p.Validate() == nil && p.Kind != "vision_projector" {
			state := "ready"
			final, _, _ := m.paths(p)
			if verifyFile(final, p) != nil || m.verifyProjector(p) != nil {
				state = "corrupt"
			}
			out = append(out, PackStatus{p, state, p.DownloadBytes()})
		}
	}
	return out, nil
}
func (m *Manager) lease(id string) (Pack, *os.File, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var p Pack
	if !uuidPattern.MatchString(id) {
		return p, nil, nil, errors.New("invalid pack ID")
	}
	if m.busy[id] {
		return p, nil, nil, errors.New("model acquisition in progress")
	}
	// A missing model must remain a read-only failure. Re-read the manifest
	// after acquiring ownership, since another application may remove it here.
	manifestPath := filepath.Join(m.root, id+".json")
	if _, err := os.Stat(manifestPath); err != nil {
		return p, nil, nil, err
	}
	acquired := false
	if m.leases[id] == 0 {
		owner, err := m.acquireOwner(id)
		if err != nil {
			return p, nil, nil, err
		}
		m.owners[id] = owner
		acquired = true
	}
	admitted := false
	defer func() {
		if acquired && !admitted {
			_ = platformfs.ReleaseLock(m.owners[id])
			delete(m.owners, id)
		}
	}()
	b, e := os.ReadFile(manifestPath)
	if e != nil {
		return p, nil, nil, e
	}
	if e = json.Unmarshal(b, &p); e != nil {
		return p, nil, nil, e
	}
	if e = p.Validate(); e != nil {
		return p, nil, nil, e
	}
	if p.ID != id {
		return p, nil, nil, errors.New("model manifest identity mismatch")
	}
	final, _, _ := m.paths(p)
	if e = verifyFile(final, p); e != nil {
		return p, nil, nil, e
	}
	f, e := os.Open(final)
	if e != nil {
		return p, nil, nil, e
	}
	m.leases[id]++
	admitted = true
	var once sync.Once
	release := func() {
		once.Do(func() {
			f.Close()
			m.mu.Lock()
			defer m.mu.Unlock()
			m.leases[id]--
			if m.leases[id] == 0 {
				_ = platformfs.ReleaseLock(m.owners[id])
				delete(m.owners, id)
			}
		})
	}
	return p, f, release, nil
}
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !uuidPattern.MatchString(id) {
		return errors.New("invalid pack ID")
	}
	if m.busy[id] || m.leases[id] > 0 {
		return errors.New("RESOURCE_EXHAUSTED: pack in use")
	}
	owner, err := m.acquireOwner(id)
	if err != nil {
		return err
	}
	defer platformfs.ReleaseLock(owner)
	if err := m.removeUnusedProjector(id); err != nil {
		return err
	}
	for _, suffix := range []string{".json", ".model", ".part", ".part.json"} {
		if e := os.Remove(filepath.Join(m.root, id+suffix)); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return nil
}
