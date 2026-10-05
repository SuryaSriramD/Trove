package native

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
)

func processRSS(pid int) (int64, error) {
	if pid <= 0 {
		return 0, errors.New("invalid worker process")
	}
	f, err := os.Open("/proc/" + strconv.Itoa(pid) + "/statm")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(raw))
	if len(raw) > 4096 || len(fields) != 7 {
		return 0, errors.New("invalid worker statm")
	}
	resident, err := strconv.ParseUint(fields[1], 10, 64)
	size := uint64(os.Getpagesize())
	if err != nil || resident == 0 || resident > (1<<63-1)/size {
		return 0, errors.New("invalid worker resident pages")
	}
	return int64(resident * size), nil
}
