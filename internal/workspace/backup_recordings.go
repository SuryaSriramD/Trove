package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type recordingBackupSource struct {
	path    string
	journal []byte
	file    *os.File
	size    int64
}

func closeBackupRecordings(sources []recordingBackupSource) {
	for _, source := range sources {
		if source.file != nil {
			source.file.Close()
		}
	}
}

// Caller holds recordingLock through this inventory and the database snapshot
// boundary. Only journal-sized reads and file opens happen while capture waits.
// Native writes are append-only; a section reader later copies a fixed prefix
// through an open descriptor even if finalization removes the source pathname.
func (k *Kernel) captureBackupRecordings(ctx context.Context) (sources []recordingBackupSource, err error) {
	defer func() {
		if err != nil {
			closeBackupRecordings(sources)
		}
	}()
	root := filepath.Join(k.path, "recordings")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, Fail("INVALID_REQUEST", "Unsafe recordings directory")
	}
	known := map[string]bool{}
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return sources, err
		}
		if strings.HasPrefix(entry.Name(), "._") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !recordingID.MatchString(id) {
			return sources, Fail("INVALID_REQUEST", "Unknown recording journal")
		}
		path, err := backupRegular(k.path, "recordings/"+entry.Name())
		if err != nil {
			return sources, err
		}
		f, err := openAsset(path)
		if err != nil {
			return sources, err
		}
		var draft RecordingDraft
		err = json.NewDecoder(io.LimitReader(f, 4<<20)).Decode(&draft)
		f.Close()
		if err != nil {
			return sources, err
		}
		if draft.ID != id {
			return sources, Fail("INVALID_REQUEST", "Recording journal identity mismatch")
		}
		switch draft.State {
		case "recording", "paused", "captured", "interrupted", "saved":
		default:
			return sources, Fail("INVALID_REQUEST", "Unknown recording state")
		}
		if draft.Bytes < 0 || draft.Bytes > 16000*4*7200 {
			return sources, Fail("INVALID_REQUEST", "Recording length is invalid")
		}
		if draft.State != "saved" {
			relative := "recordings/" + id + ".f32le"
			path, err := backupRegular(k.path, relative)
			if err != nil {
				return sources, err
			}
			f, err := openAsset(path)
			if err != nil {
				return sources, err
			}
			st, err := f.Stat()
			if err != nil {
				f.Close()
				return sources, err
			}
			size := st.Size()
			if size < draft.Bytes || size > 16000*4*7200 {
				f.Close()
				return sources, Fail("INVALID_REQUEST", "Recording length is invalid")
			}
			if draft.State == "recording" || draft.State == "paused" {
				// The live journal is unchanged. Backup-only state makes the stable,
				// acknowledged prefix recoverable without pretending capture stopped.
				size = draft.Bytes
				draft.State = "interrupted"
				draft.Error = "Backup contains the last acknowledged recording checkpoint; capture continued in the source workspace."
			}
			sources = append(sources, recordingBackupSource{path: relative, file: f, size: size})
			known[id+".f32le"] = true
		}
		journal := append(marshal(draft), '\n')
		sources = append(sources, recordingBackupSource{path: "recordings/" + entry.Name(), journal: journal, size: int64(len(journal))})
		known[entry.Name()] = true
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "._") || known[entry.Name()] || strings.HasSuffix(entry.Name(), ".wav") || strings.HasPrefix(entry.Name(), "journal-") {
			continue
		}
		return sources, Fail("INVALID_REQUEST", "Unjournaled recording data requires recovery before backup")
	}
	return sources, nil
}

func writeBackupRecordings(ctx context.Context, destination string, sources []recordingBackupSource, manifest *BackupManifest) error {
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(destination, filepath.FromSlash(source.path))
		if err := safeDirectory(filepath.Dir(path)); err != nil {
			return err
		}
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		var input io.Reader = bytes.NewReader(source.journal)
		if source.file != nil {
			input = io.NewSectionReader(source.file, 0, source.size)
		}
		hash := sha256.New()
		n, err := copyBounded(ctx, io.MultiWriter(out, hash), input, 16000*4*7200)
		if err == nil && n != source.size {
			err = Fail("INTEGRITY_ERROR", "Recording changed while copying backup")
		}
		if err == nil {
			err = out.Sync()
		}
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		manifest.Recordings = append(manifest.Recordings, BackupObject{Path: source.path, SHA256: hex.EncodeToString(hash.Sum(nil)), SizeBytes: strconv.FormatInt(n, 10)})
	}
	return validateRecordingInventory(ctx, destination, manifest.Recordings)
}
