//go:build darwin && !cgo

package native

import "errors"

func processRSS(int) (int64, error) {
	return 0, errors.New("native process memory sampling requires the CGO-enabled desktop build")
}
