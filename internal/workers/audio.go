package workers

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"trove.local/phase0/contracts"
)

// pcmInput accepts explicit raw PCM or a bounded RIFF WAV with an unambiguous
// 16 kHz mono format. Conversion occurs outside SQLite and never changes assets.
func pcmInput(source *os.File, meta contracts.DomainFile) (*os.File, func(), error) {
	cleanup := func() {}
	if meta.MediaType == "audio/x-trove-f32le" || strings.HasSuffix(strings.ToLower(meta.OriginalName), ".f32le") {
		if err := validatePCM(source); err != nil {
			return nil, cleanup, err
		}
		return source, cleanup, nil
	}
	var header [12]byte
	if _, err := source.ReadAt(header[:], 0); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return nil, cleanup, errors.New("INVALID_INPUT: speech input must be 16 kHz mono PCM WAV or .f32le")
	}
	stat, err := source.Stat()
	if err != nil {
		return nil, cleanup, err
	}
	if int64(binary.LittleEndian.Uint32(header[4:8]))+8 > stat.Size() {
		return nil, cleanup, errors.New("INVALID_INPUT: truncated WAV")
	}
	var format, channels, bits uint16
	seenFormat, seenData := false, false
	riffSize := int64(binary.LittleEndian.Uint32(header[4:8])) + 8
	var rate uint32
	var offset, size int64
	for pos := int64(12); pos+8 <= riffSize; {
		var chunk [8]byte
		if _, err = source.ReadAt(chunk[:], pos); err != nil {
			return nil, cleanup, err
		}
		n := int64(binary.LittleEndian.Uint32(chunk[4:]))
		if n > riffSize-pos-8 {
			return nil, cleanup, errors.New("INVALID_INPUT: WAV chunk exceeds file")
		}
		switch string(chunk[:4]) {
		case "fmt ":
			if seenFormat {
				return nil, cleanup, errors.New("INVALID_INPUT: repeated WAV format")
			}
			seenFormat = true
			if n < 16 || n > 1024 {
				return nil, cleanup, errors.New("INVALID_INPUT: invalid WAV format")
			}
			var f [16]byte
			if _, err = source.ReadAt(f[:], pos+8); err != nil {
				return nil, cleanup, err
			}
			format = binary.LittleEndian.Uint16(f[0:])
			channels = binary.LittleEndian.Uint16(f[2:])
			rate = binary.LittleEndian.Uint32(f[4:])
			bits = binary.LittleEndian.Uint16(f[14:])
		case "data":
			if seenData {
				return nil, cleanup, errors.New("INVALID_INPUT: multiple WAV data chunks")
			}
			seenData = true
			offset = pos + 8
			size = n
		}
		pos += 8 + n + (n % 2)
	}
	if channels != 1 || rate != 16000 || !((format == 1 && bits == 16) || (format == 3 && bits == 32)) || size < 1 || size%(int64(bits)/8) != 0 || size/(int64(bits)/8) > 16000*7200 {
		return nil, cleanup, errors.New("INVALID_INPUT: WAV must be 16 kHz mono 16-bit PCM or float32 within two hours")
	}
	out, err := os.CreateTemp("", "trove-job-audio-")
	if err != nil {
		return nil, cleanup, err
	}
	cleanup = func() { out.Close(); os.Remove(out.Name()) }
	reader := io.NewSectionReader(source, offset, size)
	buf := make([]byte, 32<<10)
	for {
		n, e := reader.Read(buf)
		if n > 0 {
			if bits == 32 {
				if _, err = out.Write(buf[:n]); err != nil {
					cleanup()
					return nil, func() {}, err
				}
			} else {
				converted := make([]byte, n*2)
				for i := 0; i+1 < n; i += 2 {
					sample := float32(int16(binary.LittleEndian.Uint16(buf[i:]))) / 32768
					binary.LittleEndian.PutUint32(converted[i*2:], math.Float32bits(sample))
				}
				if _, err = out.Write(converted); err != nil {
					cleanup()
					return nil, func() {}, err
				}
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			cleanup()
			return nil, func() {}, e
		}
	}
	if err = validatePCM(out); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return out, cleanup, nil
}

// validatePCM bounds duration and rejects nonfinite/out-of-range floats before
// a decoder/model sees bytes. A file suffix is never sufficient validation.
func validatePCM(source *os.File) error {
	st, err := source.Stat()
	if err != nil {
		return err
	}
	if st.Size() == 0 || st.Size()%4 != 0 || st.Size() > 16000*4*7200 {
		return errors.New("INVALID_INPUT: audio must contain bounded 16 kHz mono float32 PCM within two hours")
	}
	reader := io.NewSectionReader(source, 0, st.Size())
	buf := make([]byte, 32<<10)
	for {
		n, err := reader.Read(buf)
		for i := 0; i < n; i += 4 {
			value := math.Float32frombits(binary.LittleEndian.Uint32(buf[i : i+4]))
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || math.Abs(float64(value)) > 1.01 {
				return errors.New("INVALID_INPUT: audio contains invalid PCM samples")
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}
