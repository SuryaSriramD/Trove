//go:build !darwin || !cgo

package main

import "fmt"

func reminderNativeService(action int) (string, error) {
	if action == 0 {
		return "unavailable", nil
	}
	return "unavailable", fmt.Errorf("background reminder registration is implemented for macOS")
}
