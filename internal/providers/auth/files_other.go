//go:build !darwin && !linux && !windows

package auth

func readPrivateFile(string, int64) ([]byte, error) { return nil, ErrUnsupported }
func writePrivateFile(string, string, []byte) error { return ErrUnsupported }
