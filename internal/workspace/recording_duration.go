package workspace

import (
	"encoding/binary"
	"io"
	"path/filepath"
	"strconv"
)

// Called after the canonical create command has authorized and guarded the
// attached file. Read only bounded RIFF headers, never audio samples or a full
// asset hash, while holding the mutation writer. Unsupported input remains
// retained with unknown duration and is validated by the transcription decoder.
func (m *Mutation) recordingDurationMS(fileID string) (int64, error) {
	meta, err := readFileMetadata(m.Context, m.Tx, fileID)
	if err != nil {
		return 0, err
	}
	hash := string(meta.ContentHash)
	if len(hash) != 64 || meta.StorageKey != "files/"+hash[:2]+"/"+hash {
		return 0, Fail("INVALID_REQUEST", "Invalid recording asset location")
	}
	for _, dir := range []string{filepath.Join(m.Kernel.path, "files"), filepath.Join(m.Kernel.path, "files", hash[:2])} {
		if err = safeDirectory(dir); err != nil {
			return 0, err
		}
	}
	source, err := openAsset(filepath.Join(m.Kernel.path, filepath.FromSlash(meta.StorageKey)))
	if err != nil {
		return 0, err
	}
	defer source.Close()
	stat, err := source.Stat()
	if err != nil {
		return 0, err
	}
	if strconv.FormatInt(stat.Size(), 10) != meta.SizeBytes {
		return 0, Fail("INVALID_REQUEST", "Recording asset length changed")
	}
	return wavDurationMS(source, stat.Size()), nil
}

func wavDurationMS(source io.ReaderAt, fileBytes int64) int64 {
	var header [12]byte
	if fileBytes < 44 || fileBytes > 512<<20 {
		return 0
	}
	if _, err := source.ReadAt(header[:], 0); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return 0
	}
	end := int64(binary.LittleEndian.Uint32(header[4:8])) + 8
	if end < 44 || end > fileBytes {
		return 0
	}
	var format, channels, bits, alignment uint16
	var rate, byteRate uint32
	var dataBytes int64
	seenFormat, seenData := false, false
	position := int64(12)
	for chunks := 0; position+8 <= end && chunks < 256; chunks++ {
		var chunk [8]byte
		if _, err := source.ReadAt(chunk[:], position); err != nil {
			return 0
		}
		size := int64(binary.LittleEndian.Uint32(chunk[4:]))
		if size > end-position-8 {
			return 0
		}
		switch string(chunk[:4]) {
		case "fmt ":
			if seenFormat || size < 16 || size > 1024 {
				return 0
			}
			seenFormat = true
			var data [16]byte
			if _, err := source.ReadAt(data[:], position+8); err != nil {
				return 0
			}
			format = binary.LittleEndian.Uint16(data[:2])
			channels = binary.LittleEndian.Uint16(data[2:4])
			rate = binary.LittleEndian.Uint32(data[4:8])
			byteRate = binary.LittleEndian.Uint32(data[8:12])
			alignment = binary.LittleEndian.Uint16(data[12:14])
			bits = binary.LittleEndian.Uint16(data[14:16])
		case "data":
			if seenData {
				return 0
			}
			seenData = true
			dataBytes = size
		}
		position += 8 + size + size%2
	}
	if position != end || !seenFormat || !seenData || channels != 1 || rate != 16000 || !((format == 1 && bits == 16) || (format == 3 && bits == 32)) {
		return 0
	}
	if alignment != bits/8 || byteRate != rate*uint32(alignment) || dataBytes < 1 || dataBytes%int64(alignment) != 0 {
		return 0
	}
	duration := dataBytes * 1000 / int64(byteRate)
	if duration > 7200000 {
		return 0
	}
	return duration
}
