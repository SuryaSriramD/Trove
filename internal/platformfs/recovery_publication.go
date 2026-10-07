package platformfs

// Recovery publication is an append-only, positive-completion protocol for a
// newly reserved workspace directory. It never replaces an existing workspace.
// Windows remains capability-gated until native crash-order qualification.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var ErrRecoveryIncomplete = errors.New("RECOVERY_INCOMPLETE: positive publication/activation evidence is missing or inconsistent")

const recoveryPrefix = ".trove-recovery-v1"
const recoveryIntent = recoveryPrefix + ".intent.json"
const recoveryVerified = recoveryPrefix + ".verified.json"
const recoveryComplete = recoveryPrefix + ".complete.json"
const recoveryActive = recoveryPrefix + ".active.json"
const recoveryStage = recoveryPrefix + ".stage"
const maxRecoveryRecord = 32 << 20

var recoveryIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type RecoveryObject struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"sizeBytes"`
}
type RecoveryPlan struct {
	Version      int              `json:"version"`
	OperationID  string           `json:"operationId"`
	Kind         string           `json:"kind"`
	SourceID     string           `json:"sourceId"`
	SourceSHA256 string           `json:"sourceSHA256"`
	Objects      []RecoveryObject `json:"objects"`
}
type recoveryReceipt struct {
	Version      int    `json:"version"`
	OperationID  string `json:"operationId"`
	IntentSHA256 string `json:"intentSHA256"`
	RootIdentity string `json:"rootIdentity"`
	State        string `json:"state"`
}
type RecoveryStatus struct {
	OperationID  string
	State        string // absent, pending, verified, complete, active
	IntentSHA256 string
}
type RecoveryPublication struct {
	root      *os.Root
	directory string
	owner     *os.File
	plan      RecoveryPlan
	digest    string
	identity  string
	observe   func(string) error // package-only crash/fault-injection seam
}

func validateRecoveryPlan(p RecoveryPlan) error {
	if p.Version != 1 || !recoveryIDPattern.MatchString(p.OperationID) || !recoveryDigest(p.SourceSHA256) || len(p.SourceID) == 0 || len(p.SourceID) > 256 || len(p.Objects) == 0 || len(p.Objects) > 100000 {
		return ErrRecoveryIncomplete
	}
	switch p.Kind {
	case "restore", "create", "migrate", "transfer":
	default:
		return ErrRecoveryIncomplete
	}
	seen := map[string]bool{}
	for _, o := range p.Objects {
		name := o.Path
		if !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") || len(name) > 4096 || strings.HasPrefix(name, recoveryPrefix) || name == ".owner.lock" || name == "INCOMPLETE" || !recoveryDigest(o.SHA256) || o.SizeBytes < 0 || o.SizeBytes > 1<<50 {
			return ErrRecoveryIncomplete
		}
		key := strings.ToLower(name)
		if seen[key] {
			return ErrRecoveryIncomplete
		}
		seen[key] = true
		for ancestor := path.Dir(key); ancestor != "."; ancestor = path.Dir(ancestor) {
			if seen[ancestor] {
				return ErrRecoveryIncomplete
			}
		}
	}
	for key := range seen {
		for ancestor := path.Dir(key); ancestor != "."; ancestor = path.Dir(ancestor) {
			if seen[ancestor] {
				return ErrRecoveryIncomplete
			}
		}
	}
	raw, _ := json.Marshal(p)
	if len(raw) > maxRecoveryRecord {
		return ErrRecoveryIncomplete
	}
	return nil
}
func recoveryDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && strings.ToLower(s) == s
}
func recoveryHash(b []byte) string { x := sha256.Sum256(b); return hex.EncodeToString(x[:]) }

// BeginRecoveryPublication reserves a new name, takes exclusive ownership and
// durably records the intended exact payload before staging any payload bytes.
func BeginRecoveryPublication(ctx context.Context, directory string, plan RecoveryPlan) (*RecoveryPublication, error) {
	if err := RequireRecoveryDurability(); err != nil {
		return nil, err
	}
	return beginRecoveryPublication(ctx, directory, plan, nil)
}
func beginRecoveryPublication(ctx context.Context, directory string, plan RecoveryPlan, observe func(string) error) (p *RecoveryPublication, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = validateRecoveryPlan(plan); err != nil {
		return nil, err
	}
	// Mkdir reserves the final name exclusively; Windows private ACL creation is
	// supplied by the native helper without repairing any existing path.
	if err = createRecoveryDirectory(directory); err != nil {
		return nil, err
	}
	p, err = openRecoveryPublication(directory)
	if err != nil {
		return nil, err
	}
	allocated := p
	defer func() {
		if err != nil {
			allocated.Close()
		}
	}()
	raw, _ := json.Marshal(plan)
	json.Unmarshal(raw, &p.plan)
	p.observe = observe
	p.digest = recoveryHash(raw)
	if err = p.writeRecord(recoveryIntent, raw); err != nil {
		return nil, err
	}
	if err = p.checkpoint("intent"); err != nil {
		return nil, err
	}
	if err = p.checkRoot(); err != nil {
		return nil, err
	}
	if err = p.root.Mkdir(recoveryStage, 0700); err != nil {
		return nil, err
	}
	stagedDirectory, err := p.root.Open(recoveryStage)
	if err != nil {
		return nil, err
	}
	err = AuditPrivateHandle(stagedDirectory, true)
	stagedDirectory.Close()
	if err != nil {
		return nil, err
	}
	return p, nil
}
func openRecoveryPublication(directory string) (*RecoveryPublication, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	d, err := root.Open(".")
	if err == nil {
		err = AuditPrivateHandle(d, true)
		d.Close()
	}
	if err != nil {
		root.Close()
		return nil, err
	}
	owner, err := AcquireRootLock(root, ".owner.lock")
	if err != nil {
		root.Close()
		return nil, err
	}
	identity, err := RootIdentity(root)
	if err != nil {
		ReleaseLock(owner)
		root.Close()
		return nil, err
	}
	return &RecoveryPublication{root: root, directory: directory, owner: owner, identity: identity}, nil
}
func (p *RecoveryPublication) Close() error {
	if p == nil {
		return nil
	}
	e := ReleaseLock(p.owner)
	p.owner = nil
	if p.root != nil {
		e = errors.Join(e, p.root.Close())
		p.root = nil
	}
	return e
}
func (p *RecoveryPublication) checkpoint(s string) error {
	if p.observe != nil {
		return p.observe(s)
	}
	return nil
}
func (p *RecoveryPublication) checkRoot() error {
	if p.root == nil || p.owner == nil {
		return ErrRecoveryIncomplete
	}
	if err := AuditPrivateHandle(p.owner, false); err != nil {
		return err
	}
	named, err := p.root.Lstat(".owner.lock")
	if err != nil {
		return err
	}
	locked, err := p.owner.Stat()
	if err != nil {
		return err
	}
	if !named.Mode().IsRegular() || !os.SameFile(named, locked) {
		return ErrLocked
	}
	root, err := os.OpenRoot(p.directory)
	if err != nil {
		return err
	}
	defer root.Close()
	identity, err := RootIdentity(root)
	if err != nil {
		return err
	}
	if identity != p.identity {
		return ErrRecoveryIncomplete
	}
	return nil
}
func (p *RecoveryPublication) receipt(state string) recoveryReceipt {
	return recoveryReceipt{1, p.plan.OperationID, p.digest, p.identity, state}
}
func (p *RecoveryPublication) writeRecord(name string, raw []byte) error {
	if err := p.checkRoot(); err != nil {
		return err
	}
	if _, err := p.root.Lstat(name); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	temporary := name + ".staged"
	f, err := p.root.OpenFile(temporary, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if err = AuditPrivateHandle(f, false); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = p.publishRecoveryFile(temporary, name); err != nil {
		return err
	}
	return p.checkRoot()
}
func (p *RecoveryPublication) objectIndex(name string) (int, RecoveryObject, error) {
	for i, o := range p.plan.Objects {
		if o.Path == name {
			return i, o, nil
		}
	}
	return 0, RecoveryObject{}, ErrRecoveryIncomplete
}
func stagedRecoveryName(i int) string { return fmt.Sprintf("%s/%06d", recoveryStage, i) }

// Stage reads exactly the declared bytes and never accepts an oversized stream.
// A failed/partial file remains evidence and cannot be silently retried over.
func (p *RecoveryPublication) Stage(ctx context.Context, name string, input io.Reader) error {
	if err := p.checkRoot(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := p.root.Lstat(recoveryVerified); err == nil {
		return ErrRecoveryIncomplete
	} else if !os.IsNotExist(err) {
		return err
	}
	index, o, err := p.objectIndex(name)
	if err != nil {
		return err
	}
	f, err := p.root.OpenFile(stagedRecoveryName(index), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = AuditPrivateHandle(f, false); err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.CopyN(io.MultiWriter(f, h), &recoveryReader{ctx, input}, o.SizeBytes)
	if err != nil && !(err == io.EOF && n == o.SizeBytes) {
		return err
	}
	var extra [1]byte
	more, e := (&recoveryReader{ctx, input}).Read(extra[:])
	if more != 0 || e != io.EOF {
		return ErrRecoveryIncomplete
	}
	if hex.EncodeToString(h.Sum(nil)) != o.SHA256 {
		return ErrRecoveryIncomplete
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = p.verifyFile(ctx, stagedRecoveryName(index), o); err != nil {
		return err
	}
	return p.checkpoint("stage:" + name)
}

type recoveryReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *recoveryReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(b)
}
func (p *RecoveryPublication) verifyFile(ctx context.Context, name string, o RecoveryObject) error {
	info, err := p.root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrPrivateStorage
	}
	f, err := p.root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = AuditPrivateHandle(f, false); err != nil {
		return err
	}
	actual, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, actual) || actual.Size() != o.SizeBytes {
		return ErrRecoveryIncomplete
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(&recoveryReader{ctx, f}, o.SizeBytes+1))
	if err != nil {
		return err
	}
	if n != o.SizeBytes || hex.EncodeToString(h.Sum(nil)) != o.SHA256 {
		return ErrRecoveryIncomplete
	}
	return nil
}
func (p *RecoveryPublication) Verify(ctx context.Context) error {
	if err := p.checkRoot(); err != nil {
		return err
	}
	for i, o := range p.plan.Objects {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.verifyFile(ctx, stagedRecoveryName(i), o); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(p.receipt("verified"))
	if err := p.writeRecord(recoveryVerified, raw); err != nil {
		return err
	}
	return p.checkpoint("verified")
}
func (p *RecoveryPublication) Commit(ctx context.Context) error {
	if err := RequireRecoveryDurability(); err != nil {
		return err
	}
	return p.commit(ctx)
}
func (p *RecoveryPublication) commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.checkRoot(); err != nil {
		return err
	}
	if _, err := p.root.Lstat(recoveryComplete); err == nil {
		_, err = InspectRecoveryAdmission(p.directory, p.plan.OperationID)
		return err
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := p.readReceipt(recoveryVerified, "verified"); err != nil {
		return err
	}
	for i, o := range p.plan.Objects {
		if err := ctx.Err(); err != nil {
			return err
		}
		stage := stagedRecoveryName(i)
		_, targetErr := p.root.Lstat(o.Path)
		_, stageErr := p.root.Lstat(stage)
		if targetErr == nil {
			// A crash can leave the expected target OR its staged source, never both.
			if !os.IsNotExist(stageErr) {
				return ErrRecoveryIncomplete
			}
			if err := p.verifyFile(ctx, o.Path, o); err != nil {
				return err
			}
			continue
		}
		if !os.IsNotExist(targetErr) {
			return targetErr
		}
		if stageErr != nil {
			return stageErr
		}
		if err := p.verifyFile(ctx, stage, o); err != nil {
			return err
		}
		if err := p.publishRecoveryFile(stage, o.Path); err != nil {
			return err
		}
		if err := p.verifyFile(ctx, o.Path, o); err != nil {
			return err
		}
		if err := p.checkpoint("publish:" + o.Path); err != nil {
			return err
		}
	}
	if err := p.verifyPayload(ctx); err != nil {
		return err
	}
	if err := syncRecoveryTreeForPublication(p.directory); err != nil {
		return err
	}
	if err := SyncDirectory(filepath.Dir(p.directory)); err != nil {
		return err
	}
	if err := p.checkpoint("before_complete"); err != nil {
		return err
	}
	raw, _ := json.Marshal(p.receipt("complete"))
	if err := p.writeRecord(recoveryComplete, raw); err != nil {
		return err
	}
	return p.checkpoint("complete")
}
func (p *RecoveryPublication) verifyPayload(ctx context.Context) error {
	if err := recoveryStageEmpty(p.root); err != nil {
		return err
	}
	wanted := map[string]bool{}
	allowedDirs := map[string]bool{".": true}
	for _, o := range p.plan.Objects {
		wanted[o.Path] = true
		for parent := path.Dir(o.Path); parent != "."; parent = path.Dir(parent) {
			allowedDirs[parent] = true
		}
	}
	if err := fs.WalkDir(p.root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrPrivateStorage
		}
		if name == recoveryStage {
			if !entry.IsDir() {
				return ErrRecoveryIncomplete
			}
			return fs.SkipDir
		}
		if entry.IsDir() {
			if !allowedDirs[name] {
				return ErrRecoveryIncomplete
			}
			return nil
		}
		if name == ".owner.lock" || name == recoveryIntent || name == recoveryVerified || name == recoveryComplete || name == recoveryActive {
			return nil
		}
		if !wanted[name] {
			return ErrRecoveryIncomplete
		}
		return nil
	}); err != nil {
		return err
	}
	for _, name := range []string{"workspace.db-wal", "workspace.db-shm", "workspace.db-journal"} {
		if _, err := p.root.Lstat(name); !os.IsNotExist(err) {
			return ErrRecoveryIncomplete
		}
	}
	for _, o := range p.plan.Objects {
		if err := p.verifyFile(ctx, o.Path, o); err != nil {
			return err
		}
	}
	return nil
}
func recoveryReadJSON(root *os.Root, name string, out any) ([]byte, error) {
	st, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > maxRecoveryRecord {
		return nil, ErrRecoveryIncomplete
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err = AuditPrivateHandle(f, false); err != nil {
		return nil, err
	}
	actual, err := f.Stat()
	if err != nil || !os.SameFile(st, actual) {
		return nil, ErrRecoveryIncomplete
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxRecoveryRecord+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxRecoveryRecord {
		return nil, ErrRecoveryIncomplete
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(out); err != nil {
		return nil, ErrRecoveryIncomplete
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, ErrRecoveryIncomplete
	}
	return raw, nil
}
func (p *RecoveryPublication) readReceipt(name, state string) error {
	var receipt recoveryReceipt
	_, err := recoveryReadJSON(p.root, name, &receipt)
	if err != nil {
		return err
	}
	if receipt != p.receipt(state) {
		return ErrRecoveryIncomplete
	}
	return nil
}
func loadRecoveryPlan(p *RecoveryPublication, expectedID string) error {
	raw, err := recoveryReadJSON(p.root, recoveryIntent, &p.plan)
	if err != nil {
		return err
	}
	if err = validateRecoveryPlan(p.plan); err != nil {
		return err
	}
	if p.plan.OperationID != expectedID {
		return ErrRecoveryIncomplete
	}
	p.digest = recoveryHash(raw)
	return nil
}

// Recover explicitly resumes a verified, partially published new destination.
// Startup only validates; it never invokes this mutation implicitly.
func RecoverPublication(ctx context.Context, directory, expectedID string) error {
	if err := RequireRecoveryDurability(); err != nil {
		return err
	}
	p, err := openRecoveryPublication(directory)
	if err != nil {
		return err
	}
	defer p.Close()
	if err = loadRecoveryPlan(p, expectedID); err != nil {
		return err
	}
	if _, err = p.root.Lstat(recoveryComplete); err == nil {
		_, err = InspectRecoveryAdmission(directory, expectedID)
		return err
	} else if !os.IsNotExist(err) {
		return err
	}
	return p.commit(ctx)
}

// InspectRecoveryAdmission must run while the caller holds the workspace owner
// lock, before SQLite can mutate. expectedID is read from the database read-only;
// it detects a missing journal even when every sidecar directory entry was lost.
func InspectRecoveryAdmission(directory, expectedID string) (RecoveryStatus, error) {
	status := RecoveryStatus{State: "absent"}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return status, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return status, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return status, err
	}
	has := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), recoveryPrefix) {
			has = true
		}
	}
	if !has && expectedID == "" {
		return status, nil
	}
	if !has || !recoveryIDPattern.MatchString(expectedID) {
		return status, ErrRecoveryIncomplete
	}
	if err = AuditPrivateHandle(dir, true); err != nil {
		return status, err
	}
	identity, err := RootIdentity(root)
	if err != nil {
		return status, err
	}
	p := &RecoveryPublication{root: root, directory: directory, identity: identity}
	if err = loadRecoveryPlan(p, expectedID); err != nil {
		return status, errors.Join(ErrRecoveryIncomplete, err)
	}
	status = RecoveryStatus{OperationID: expectedID, State: "pending", IntentSHA256: p.digest}
	if err = p.readReceipt(recoveryVerified, "verified"); err != nil {
		return status, errors.Join(ErrRecoveryIncomplete, err)
	}
	status.State = "verified"
	if err = p.readReceipt(recoveryComplete, "complete"); err != nil {
		return status, errors.Join(ErrRecoveryIncomplete, err)
	}
	status.State = "complete"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), recoveryPrefix) && entry.Name() != recoveryIntent && entry.Name() != recoveryVerified && entry.Name() != recoveryComplete && entry.Name() != recoveryActive && entry.Name() != recoveryStage {
			return status, ErrRecoveryIncomplete
		}
	}
	if err = recoveryStageEmpty(root); err != nil {
		return status, errors.Join(ErrRecoveryIncomplete, err)
	}
	if _, err = root.Lstat(recoveryActive); err == nil {
		if err = p.readReceipt(recoveryActive, "active"); err != nil {
			return status, err
		}
		status.State = "active"
		return status, nil
	} else if !os.IsNotExist(err) {
		return status, err
	}
	if err = p.verifyPayload(context.Background()); err != nil {
		return status, errors.Join(ErrRecoveryIncomplete, err)
	}
	return status, nil
}

// ActivateRecoveryAdmission records successful baseline verification before any
// normal database edit. Later opens validate receipts without hashing a mutable
// database against its original restoration bytes. Caller retains owner lock.
func ActivateRecoveryAdmission(directory, expectedID string, owner *os.File) error {
	if err := RequireRecoveryDurability(); err != nil {
		return err
	}
	status, err := InspectRecoveryAdmission(directory, expectedID)
	if err != nil {
		return err
	}
	if status.State == "absent" || status.State == "active" {
		return nil
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	identity, err := RootIdentity(root)
	if err != nil {
		return err
	}
	p := &RecoveryPublication{root: root, directory: directory, identity: identity, owner: owner}
	if owner == nil {
		return ErrLocked
	}
	if err = AuditPrivateHandle(owner, false); err != nil {
		return err
	}
	named, err := root.Lstat(".owner.lock")
	if err != nil {
		return err
	}
	opened, err := owner.Stat()
	if err != nil || !os.SameFile(named, opened) {
		return ErrLocked
	}
	if err = loadRecoveryPlan(p, expectedID); err != nil {
		return err
	}
	raw, _ := json.Marshal(p.receipt("active"))
	return p.writeRecord(recoveryActive, raw)
}
func syncRecoveryTreeForPublication(directory string) error {
	dirs := []string{}
	err := filepath.WalkDir(directory, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.Type()&os.ModeSymlink != 0 {
			return ErrPrivateStorage
		}
		if e.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		if err = SyncDirectory(d); err != nil {
			return err
		}
	}
	return nil
}

// RelocateRecoveryPublication moves the entire completed, still-unactivated
// directory on one volume. Its inode/volume identity and owner lock survive the
// move; copying receipt files into a different directory never grants admission.
// The caller (owner transfer) must durably fence its old owner first.
func RelocateRecoveryPublication(ctx context.Context, source, destination, expectedID string) error {
	if err := RequireRecoveryDurability(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := openRecoveryPublication(source)
	if err != nil {
		return err
	}
	defer p.Close()
	status, err := InspectRecoveryAdmission(source, expectedID)
	if err != nil {
		return err
	}
	if status.State != "complete" {
		return ErrRecoveryIncomplete
	}
	if err = p.checkRoot(); err != nil {
		return err
	}
	if err = relocateRecoveryDirectory(source, destination); err != nil {
		return err
	}
	p.directory = destination
	return p.checkRoot()
}

func recoveryStageEmpty(root *os.Root) error {
	info, err := root.Lstat(recoveryStage)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrRecoveryIncomplete
	}
	stage, err := root.Open(recoveryStage)
	if err != nil {
		return err
	}
	defer stage.Close()
	if err = AuditPrivateHandle(stage, true); err != nil {
		return err
	}
	entries, err := stage.ReadDir(1)
	if len(entries) != 0 || (err != nil && err != io.EOF) {
		return ErrRecoveryIncomplete
	}
	return nil
}
