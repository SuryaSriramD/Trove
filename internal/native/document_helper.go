package native

import (
	"context"
	"crypto/sha256"
	"debug/elf"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"trove.local/phase0/internal/platformfs"
)

type DocumentResource struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type documentManifest struct {
	SchemaVersion      int                         `json:"schemaVersion"`
	Name               string                      `json:"name"`
	OS                 string                      `json:"os"`
	Architecture       string                      `json:"architecture"`
	Runtime            string                      `json:"runtime"`
	SHA256             string                      `json:"sha256"`
	TestOnly           *bool                       `json:"testOnly"`
	WebExtensionSHA256 string                      `json:"webExtensionSha256,omitempty"`
	MinimumWebView2    string                      `json:"minimumWebView2,omitempty"`
	Assets             map[string]DocumentResource `json:"assets"`
}
type DocumentHelper struct {
	path, bundle string
	manifest     documentManifest
}
type DocumentJob struct {
	helper                          *DocumentHelper
	private, root, executable, mode string
	files                           map[string]DocumentResource
	cleanupUnsafe                   bool
}

func documentRuntime(platform string) string {
	switch platform {
	case "windows":
		return "webview2-pdfjs-6.3.289-v1"
	case "linux":
		return "webkitgtk-2.52.6-pdfjs-6.3.289-v1"
	}
	return ""
}
func documentName(platform string) string {
	if platform == "windows" {
		return "document-worker.exe"
	}
	return "document-worker"
}
func documentHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && s == strings.ToLower(s)
}
func documentPath(name string) bool {
	if name == "" || len(name) > 512 || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("/._-", c)) {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			return false
		}
	}
	return true
}
func documentAsset(name string) bool {
	if !documentPath(name) {
		return false
	}
	if name == "print.html" || name == "extract.html" || name == "validate.html" {
		return true
	}
	if !strings.HasPrefix(name, "assets/") && !strings.HasPrefix(name, "pdfjs-assets/") {
		return false
	}
	switch filepath.Ext(name) {
	case ".js", ".mjs", ".css", ".json", ".woff", ".woff2", ".ttf", ".otf", ".pfb", ".bcmap", ".icc", ".wasm", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return true
	}
	return false
}
func readDocumentManifest(ctx context.Context, path, platform, arch string) (documentManifest, error) {
	var m documentManifest
	f, err := platformfs.OpenRegular(path)
	if err != nil {
		return m, err
	}
	defer f.Close()
	raw, err := io.ReadAll(&ocrContextReader{ctx: ctx, r: io.LimitReader(f, (1<<20)+1)})
	if err != nil {
		return m, err
	}
	if len(raw) > 1<<20 {
		return m, errors.New("document manifest exceeds limit")
	}
	if err = json.Unmarshal(raw, &m); err != nil {
		return m, errors.New("invalid document helper manifest")
	}
	if m.SchemaVersion != 1 || m.Name != "document-worker" || m.OS != platform || m.Architecture != arch || arch != "amd64" || m.Runtime == "" || m.Runtime != documentRuntime(platform) || m.TestOnly == nil || *m.TestOnly || !documentHash(m.SHA256) {
		return m, errors.New("incompatible document helper identity")
	}
	if platform == "windows" && m.MinimumWebView2 != "141.0.3537.50" {
		return m, errors.New("document WebView2 minimum differs from pinned SDK")
	}
	if platform == "linux" && !documentHash(m.WebExtensionSHA256) {
		return m, errors.New("document network policy extension is missing")
	}
	if len(m.Assets) < 3 || len(m.Assets) > 4096 {
		return m, errors.New("document asset count exceeds bounds")
	}
	seen := map[string]bool{}
	var total int64
	for name, entry := range m.Assets {
		fold := strings.ToLower(name)
		if !documentAsset(name) || seen[fold] || entry.Size < 0 || entry.Size > 64<<20 || !documentHash(entry.SHA256) {
			return m, errors.New("invalid document asset identity")
		}
		seen[fold] = true
		total += entry.Size
	}
	if total > 128<<20 {
		return m, errors.New("document assets exceed128MiB")
	}
	for _, name := range []string{"print.html", "extract.html", "validate.html"} {
		if _, ok := m.Assets[name]; !ok {
			return m, errors.New("document entry missing")
		}
	}
	return m, nil
}
func documentBinaryTarget(f *os.File, platform string) bool {
	if platform == "windows" {
		binary, err := pe.NewFile(f)
		if err != nil {
			return false
		}
		return binary.Machine == pe.IMAGE_FILE_MACHINE_AMD64
	}
	if platform == "linux" {
		binary, err := elf.NewFile(f)
		if err != nil {
			return false
		}
		return binary.Class == elf.ELFCLASS64 && binary.Data == elf.ELFDATA2LSB && binary.Machine == elf.EM_X86_64
	}
	return false
}

// FindDocumentHelper distinguishes absent optional capability from a damaged or
// incompatible installed helper, which must never silently fall back.
func FindDocumentHelper(ctx context.Context) (*DocumentHelper, error) {
	return findDocumentHelperForPlatform(ctx, runtime.GOOS, runtime.GOARCH)
}

// An unavailable platform is rejected before inspecting helper paths or user input.
func findDocumentHelperForPlatform(ctx context.Context, platform, arch string) (*DocumentHelper, error) {
	if err := documentPlatformCapability(platform); err != nil {
		return nil, err
	}
	name := documentName(platform)
	paths := []string{}
	if path := os.Getenv("TROVE_DOCUMENT_WORKER"); path != "" {
		paths = append(paths, path)
	}
	if bundle := os.Getenv("TROVE_NATIVE_BUNDLE"); bundle != "" {
		paths = append(paths, filepath.Join(bundle, name))
	}
	executable, _ := os.Executable()
	paths = append(paths, filepath.Join(filepath.Dir(executable), "../Resources/native", name), filepath.Join(filepath.Dir(executable), "native", name))
	if cwd, err := os.Getwd(); err == nil {
		for i := 0; i < 6; i++ {
			paths = append(paths, filepath.Join(cwd, "build/native", name))
			next := filepath.Dir(cwd)
			if next == cwd {
				break
			}
			cwd = next
		}
	}
	for _, path := range paths {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		bundle := filepath.Dir(path)
		manifest, err := readDocumentManifest(ctx, filepath.Join(bundle, "document-worker.json"), platform, arch)
		if err != nil {
			return nil, err
		}
		f, err := verifiedOCRFile(ctx, path, manifest.SHA256, 128<<20)
		if err != nil {
			return nil, err
		}
		target := documentBinaryTarget(f, platform)
		f.Close()
		if !target {
			return nil, errors.New("document helper binary target mismatch")
		}
		return &DocumentHelper{path: path, bundle: bundle, manifest: manifest}, nil
	}
	return nil, nil
}
func (h *DocumentHelper) NewJob(ctx context.Context, mode string) (*DocumentJob, error) {
	return h.newJobForPlatform(ctx, mode, runtime.GOOS)
}
func (h *DocumentHelper) newJobForPlatform(ctx context.Context, mode, platform string) (job *DocumentJob, err error) {
	if err = documentPlatformCapability(platform); err != nil {
		return nil, err
	}
	if mode != "render" && mode != "extract" {
		return nil, errors.New("invalid document mode")
	}
	private, err := newDocumentDirectory()
	if err != nil {
		return nil, err
	}
	job = &DocumentJob{helper: h, private: private, root: filepath.Join(private, "document"), executable: filepath.Join(private, documentName(runtime.GOOS)), mode: mode, files: map[string]DocumentResource{}}
	defer func() {
		if err != nil {
			os.RemoveAll(private)
		}
	}()
	if err = documentMkdir(job.root); err != nil {
		return nil, err
	}
	if err = documentMkdir(filepath.Join(private, "webview-profile")); err != nil {
		return nil, err
	}
	binary, err := verifiedOCRFile(ctx, h.path, h.manifest.SHA256, 128<<20)
	if err != nil {
		return nil, err
	}
	defer binary.Close()
	out, err := os.OpenFile(job.executable, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(out, &ocrContextReader{ctx: ctx, r: io.LimitReader(binary, (128<<20)+1)})
	closeErr := out.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if n > 128<<20 {
		return nil, errors.New("document helper exceeds limit")
	}
	copied, err := verifiedOCRFile(ctx, job.executable, h.manifest.SHA256, 128<<20)
	if err != nil {
		return nil, err
	}
	copied.Close()
	if h.manifest.OS == "linux" {
		extensionDir := filepath.Join(private, "web-extensions")
		if err = documentMkdir(extensionDir); err != nil {
			return nil, err
		}
		extension, er := verifiedOCRFile(ctx, filepath.Join(h.bundle, "document-web-extension.so"), h.manifest.WebExtensionSHA256, 16<<20)
		if er != nil {
			return nil, er
		}
		target := filepath.Join(extensionDir, "document-web-extension.so")
		output, er := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if er == nil {
			var n int64
			n, er = io.Copy(output, &ocrContextReader{ctx: ctx, r: io.LimitReader(extension, (16<<20)+1)})
			er = errors.Join(er, output.Close())
			if n > 16<<20 {
				er = errors.New("document extension exceeds bound")
			}
		}
		extension.Close()
		if er != nil {
			return nil, er
		}
		checked, er := verifiedOCRFile(ctx, target, h.manifest.WebExtensionSHA256, 16<<20)
		if er != nil {
			return nil, er
		}
		checked.Close()
	}
	for name, entry := range h.manifest.Assets {
		if mode == "render" && name == "extract.html" || mode == "extract" && (name == "print.html" || name == "validate.html") {
			continue
		}
		path := filepath.Join(h.bundle, "document-assets", filepath.FromSlash(name))
		if err = checkDocumentParents(filepath.Join(h.bundle, "document-assets"), name); err != nil {
			return nil, err
		}
		f, er := platformfs.OpenRegular(path)
		if er != nil {
			return nil, er
		}
		err = job.AddFile(ctx, name, f, entry.Size, entry.SHA256)
		f.Close()
		if err != nil {
			return nil, err
		}
	}
	return job, nil
}
func checkDocumentParents(root, name string) error {
	current := root
	parts := strings.Split(name, "/")
	for i := -1; i < len(parts)-1; i++ {
		if i >= 0 {
			current = filepath.Join(current, parts[i])
		}
		st, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return errors.New("document asset parent is not a directory")
		}
	}
	return nil
}
func (j *DocumentJob) Root() string { return j.root }
func (j *DocumentJob) Close() error {
	if j == nil || j.private == "" {
		return nil
	}
	if j.cleanupUnsafe {
		return errors.New("temporary document files retained because browser process exit could not be verified")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := os.RemoveAll(j.private); err == nil {
			j.private = ""
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("temporary document profile cleanup failed; local temporary files remain")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
func (j *DocumentJob) AddFile(ctx context.Context, name string, input io.Reader, maximum int64, expectedSHA string) error {
	if !documentPath(name) || j.files[name].SHA256 != "" || len(j.files) >= 4096 || maximum < 0 || maximum > 128<<20 {
		return errors.New("invalid document resource")
	}
	path := filepath.Join(j.root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), &ocrContextReader{ctx: ctx, r: io.LimitReader(input, maximum+1)})
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	if n > maximum {
		return errors.New("document resource exceeds bound")
	}
	digest := hex.EncodeToString(h.Sum(nil))
	if expectedSHA != "" && digest != expectedSHA {
		return errors.New("document resource changed during staging")
	}
	j.files[name] = DocumentResource{SHA256: digest, Size: n}
	return nil
}
func (j *DocumentJob) Run(ctx context.Context, output io.Writer) error {
	return j.runForPlatform(ctx, output, runtime.GOOS)
}
func (j *DocumentJob) runForPlatform(ctx context.Context, output io.Writer, platform string) error {
	if err := documentPlatformCapability(platform); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	raw, err := json.Marshal(map[string]any{"schemaVersion": 1, "files": j.files})
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return errors.New("document resource map exceeds bound")
	}
	f, err := os.OpenFile(filepath.Join(j.root, "resource-map.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	limit := int64(16 << 20)
	if j.mode == "render" {
		limit = 256 << 20
	}
	return j.recordProcessResult(runDocumentProcess(ctx, j.executable, j.mode, j.root, j.private, &documentOutput{w: output, remaining: limit}))
}

type documentOutput struct {
	w         io.Writer
	remaining int64
}

func (o *documentOutput) Write(p []byte) (int, error) {
	if int64(len(p)) > o.remaining {
		return 0, errors.New("document output exceeds bound")
	}
	n, err := o.w.Write(p)
	o.remaining -= int64(n)
	return n, err
}

func (j *DocumentJob) recordProcessResult(err error) error {
	if errors.Is(err, errDocumentCleanupUnverified) {
		j.cleanupUnsafe = true
	}
	return err
}
