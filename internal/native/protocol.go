package native

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

const MaxFrameBytes = 1 << 20

// ReadFrame checks length before allocation and bounds every underlying read.
func ReadFrame(r io.Reader) (json.RawMessage, error) {
	var size [4]byte
	if _, e := io.ReadFull(r, size[:]); e != nil {
		return nil, e
	}
	n := binary.BigEndian.Uint32(size[:])
	if n == 0 || n > MaxFrameBytes {
		return nil, errors.New("invalid native frame length")
	}
	data := make([]byte, n)
	for at := 0; at < len(data); {
		end := at + 65536
		if end > len(data) {
			end = len(data)
		}
		read, e := io.ReadFull(r, data[at:end])
		at += read
		if e != nil {
			return nil, e
		}
	}
	if !utf8.Valid(data) || !json.Valid(data) {
		return nil, errors.New("invalid native JSON frame")
	}
	return data, nil
}
func WriteFrame(w io.Writer, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if len(b) == 0 || len(b) > MaxFrameBytes {
		return errors.New("native frame exceeds limit")
	}
	var head [4]byte
	binary.BigEndian.PutUint32(head[:], uint32(len(b)))
	if _, e = w.Write(head[:]); e != nil {
		return e
	}
	for len(b) > 0 {
		n := len(b)
		if n > 65536 {
			n = 65536
		}
		k, e := w.Write(b[:n])
		if e != nil {
			return e
		}
		if k == 0 {
			return io.ErrShortWrite
		}
		b = b[k:]
	}
	return nil
}
