package processdomain

import (
	"errors"
	"strconv"
	"strings"
)

func parseUnsigned(raw string) (uint64, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return 0, errors.New("invalid aggregate counter")
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, errors.New("invalid aggregate counter")
		}
	}
	return strconv.ParseUint(raw, 10, 64)
}
func parseCounters(raw string) (map[string]uint64, error) {
	if len(raw) > 16<<10 {
		return nil, errors.New("oversized aggregate counters")
	}
	result := map[string]uint64{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, errors.New("malformed aggregate counters")
		}
		if _, exists := result[fields[0]]; exists {
			return nil, errors.New("duplicate aggregate counter")
		}
		value, err := parseUnsigned(fields[1])
		if err != nil {
			return nil, err
		}
		result[fields[0]] = value
	}
	return result, nil
}
