package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const ocrModelSHA256 = "7d4322bd2a7749724879683fc3912cb542f19906c83bcc1a52132556427170b2"
const portableOCRVersion = "trove-tesseract-5.5.1-eng-" + ocrModelSHA256
const portableOCRWarning = "English printed text only. Encoded image orientation is retained; rotated text, handwriting, columns and low contrast may be missed. Alpha is composited over white; verify against the original."

type OCRCapability struct {
	Engine           string   `json:"engine"`
	ExtractorVersion string   `json:"extractorVersion"`
	Languages        []string `json:"languages"`
	Limitations      string   `json:"limitations"`
	ModelSHA256      string   `json:"modelSha256,omitempty"`
}
type ocrManifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Name          string `json:"name"`
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
	Runtime       string `json:"runtime"`
	SHA256        string `json:"sha256"`
	ModelSHA256   string `json:"modelSha256"`
	TestOnly      bool   `json:"testOnly"`
}

func portableOCRCapability() OCRCapability {
	return OCRCapability{Engine: "Tesseract 5.5.1 (local)", ExtractorVersion: portableOCRVersion, Languages: []string{"English"}, Limitations: portableOCRWarning, ModelSHA256: ocrModelSHA256}
}
func portableOCRName() string {
	if runtime.GOOS == "windows" {
		return "ocr-worker.exe"
	}
	return "ocr-worker"
}
func readOCRManifest(bundle string) (ocrManifest, error) {
	var m ocrManifest
	path := filepath.Join(bundle, "ocr-worker.json")
	if err := regular(path); err != nil {
		return m, err
	}
	f, err := os.Open(path)
	if err != nil {
		return m, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return m, err
	}
	if len(data) > 65536 {
		return m, errors.New("OCR manifest too large")
	}
	if err = json.Unmarshal(data, &m); err != nil {
		return m, errors.New("invalid OCR manifest")
	}
	if m.SchemaVersion != 1 || m.Name != "ocr-worker" || m.OS != runtime.GOOS || m.Architecture != runtime.GOARCH || m.Runtime != "tesseract-5.5.1-leptonica-1.85.0-eng-v1" || m.TestOnly || len(m.SHA256) != 64 || m.ModelSHA256 != ocrModelSHA256 {
		return m, errors.New("incompatible OCR helper or model")
	}
	if _, err = hex.DecodeString(m.SHA256); err != nil {
		return m, errors.New("invalid OCR helper digest")
	}
	return m, nil
}
func verifiedOCRFile(ctx context.Context, path, digest string, max int64) (*os.File, error) {
	if err := regular(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*os.File, error) { f.Close(); return nil, err }
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > max {
		return fail(errors.New("OCR artifact exceeds bounds"))
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	var total int64
	for {
		if err = ctx.Err(); err != nil {
			return fail(err)
		}
		n, e := f.Read(buf)
		total += int64(n)
		if total > max {
			return fail(errors.New("OCR artifact grew beyond bound"))
		}
		if n > 0 {
			h.Write(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return fail(e)
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return fail(errors.New("OCR artifact digest mismatch"))
	}
	_, err = f.Seek(0, 0)
	if err != nil {
		return fail(err)
	}
	return f, nil
}

// OCRCapabilities reports only a verified installed engine, never a platform guess.
func OCRCapabilities(bundle string) (OCRCapability, error) {
	if _, err := os.Lstat(filepath.Join(bundle, "ocr-worker.json")); !os.IsNotExist(err) {
		m, err := readOCRManifest(bundle)
		if err != nil {
			return OCRCapability{}, err
		}
		f, err := verifiedOCRFile(context.Background(), filepath.Join(bundle, portableOCRName()), m.SHA256, 128<<20)
		if err != nil {
			return OCRCapability{}, err
		}
		f.Close()
		f, err = verifiedOCRFile(context.Background(), filepath.Join(bundle, "eng.traineddata"), m.ModelSHA256, 32<<20)
		if err != nil {
			return OCRCapability{}, err
		}
		f.Close()
		return portableOCRCapability(), nil
	}
	if err := verifyAppleOCRHelper(bundle); err != nil {
		return OCRCapability{}, err
	}
	return OCRCapability{Engine: "Apple Vision (local)", ExtractorVersion: "trove-local-vision-v1", Languages: []string{"Automatic detection (OS-supported languages)"}, Limitations: "Handwriting, columns, low contrast and rotation may be inaccurate; verify against the original."}, nil
}

// prepareOCRPixels keeps full decoded dimensions. There is no interpolation,
// sharpening, EXIF rotation, or lossy resize presented as source text.
func prepareOCRPixels(ctx context.Context, input *os.File, output *os.File) error {
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 20<<20 {
		return errors.New("OCR requires a PNG/JPEG up to 20 MiB")
	}
	if _, err = input.Seek(0, 0); err != nil {
		return err
	}
	data, err := io.ReadAll(&ocrContextReader{ctx: ctx, r: io.LimitReader(input, (20<<20)+1)})
	if err != nil {
		return err
	}
	if len(data) > 20<<20 {
		return errors.New("OCR image exceeds 20 MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid OCR image")
	}
	if format != "png" && format != "jpeg" || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 20000000 {
		return errors.New("OCR requires PNG/JPEG with at most 20 megapixels")
	}
	img, _, err := image.Decode(&ocrContextReader{ctx: ctx, r: bytes.NewReader(data)})
	if err != nil {
		return err
	}
	b := img.Bounds()
	if b.Dx() != config.Width || b.Dy() != config.Height {
		return errors.New("OCR dimensions changed while decoding")
	}
	header := make([]byte, 12)
	copy(header, "TOCR")
	binary.BigEndian.PutUint32(header[4:8], uint32(b.Dx()))
	binary.BigEndian.PutUint32(header[8:12], uint32(b.Dy()))
	if _, err = output.Write(header); err != nil {
		return err
	}
	row := make([]byte, b.Dx())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			white := uint32(65535) - a
			// RGBA returns premultiplied values. Composite white before luminance.
			gray := (uint64(r+white)*19595 + uint64(g+white)*38470 + uint64(bl+white)*7471 + 32768) >> 16
			row[x-b.Min.X] = byte((gray + 128) / 257)
		}
		if _, err = output.Write(row); err != nil {
			return err
		}
	}
	_, err = output.Seek(0, 0)
	return err
}

type ocrContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *ocrContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func runPortableOCR(ctx context.Context, input *os.File, bundle string) (OCRResult, error) {
	var result OCRResult
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	m, err := readOCRManifest(bundle)
	if err != nil {
		return result, err
	}
	helper, err := verifiedOCRFile(ctx, filepath.Join(bundle, portableOCRName()), m.SHA256, 128<<20)
	if err != nil {
		return result, err
	}
	defer helper.Close()
	model, err := verifiedOCRFile(ctx, filepath.Join(bundle, "eng.traineddata"), m.ModelSHA256, 32<<20)
	if err != nil {
		return result, err
	}
	defer model.Close()
	private, err := privateWorkerDirectory("trove-ocr-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(private)
	executable := filepath.Join(private, portableOCRName())
	dst, err := os.OpenFile(executable, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return result, err
	}
	// Copy the exact already-verified open handle and verify the private copy too.
	copied, err := io.Copy(dst, &ocrContextReader{ctx: ctx, r: io.LimitReader(helper, (128<<20)+1)})
	closeErr := dst.Close()
	if copied > 128<<20 {
		return result, errors.New("OCR executable grew beyond bound")
	}
	if err != nil {
		return result, err
	}
	if closeErr != nil {
		return result, closeErr
	}
	verified, err := verifiedOCRFile(ctx, executable, m.SHA256, 128<<20)
	if err != nil {
		return result, err
	}
	verified.Close()
	pixels, err := os.OpenFile(filepath.Join(private, "pixels"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return result, err
	}
	defer pixels.Close()
	if err = prepareOCRPixels(ctx, input, pixels); err != nil {
		return result, err
	}
	cmd := exec.Command(executable)
	cmd.Dir = private
	cmd.Env = workerEnvironment(private)
	raw, err := runOCRProcess(ctx, cmd, []*os.File{pixels, model})
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, errors.New("invalid local OCR response")
	}
	result.Warning = portableOCRWarning
	result.Capability = portableOCRCapability()
	return result, nil
}

// The caller has verified the executable and opened only its approved inputs.
func runOCRProcess(ctx context.Context, cmd *exec.Cmd, files []*os.File) ([]byte, error) {
	var output ocrOutput
	cmd.Stdout = &output
	var diagnostic ringLog
	cmd.Stderr = &diagnostic
	start, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	defer start.Close()
	process, err := prepareWorker(cmd, files, 1<<30)
	if err != nil {
		return nil, err
	}
	defer process.close()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = process.start(); err != nil {
		return nil, errors.New("cannot start local OCR helper")
	}
	// Windows job assignment has succeeded before the worker may read any input.
	if _, err = start.Write([]byte{1}); err != nil {
		process.stop(true)
		cmd.Wait()
		return nil, errors.New("cannot release OCR start barrier")
	}
	start.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err = <-done:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if err != nil {
				return nil, errors.New("local OCR worker failed")
			}
			return output.Bytes(), nil
		case <-ctx.Done():
			process.stop(true)
			<-done
			return nil, ctx.Err()
		case <-tick.C:
			rss, e := process.rss()
			if e == nil && rss > 1<<30 {
				process.stop(true)
				<-done
				return nil, errors.New("OCR exceeded 1 GiB memory bound")
			}
		}
	}
}

func hasPortableOCR(bundle string) bool {
	_, err := os.Lstat(filepath.Join(bundle, "ocr-worker.json"))
	return !os.IsNotExist(err)
}
