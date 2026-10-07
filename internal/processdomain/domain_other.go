//go:build !linux

package processdomain

import "fmt"

func New(config Config) (ProcessDomain, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w: owned cgroup-v2 domains require Linux", ErrUnavailable)
}
