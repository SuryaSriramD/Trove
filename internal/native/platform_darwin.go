package native

import (
	"os/exec"
	"strconv"
	"strings"
)

func platformMemoryBytes() (int64, error) {
	raw, err := exec.Command("/usr/sbin/sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
}
