package workspace

import (
	"context"
	"io"
	"time"
)

type importPreviewReader struct {
	ctx      context.Context
	reader   io.Reader
	read     int64
	last     time.Time
	progress func(int64)
}

func (r *importPreviewReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	r.read += int64(n)
	if time.Since(r.last) >= 100*time.Millisecond || err == io.EOF {
		r.progress(r.read)
		r.last = time.Now()
	}
	return n, err
}
