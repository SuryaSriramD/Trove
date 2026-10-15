//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework Foundation -framework ServiceManagement
#include <stdlib.h>
char *trove_reminder_service(int action);
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"unsafe"
)

func reminderNativeService(action int) (string, error) {
	raw := C.trove_reminder_service(C.int(action))
	if raw == nil {
		return "unknown", fmt.Errorf("background reminder service is unavailable")
	}
	defer C.free(unsafe.Pointer(raw))
	var reply struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(C.GoString(raw)), &reply); err != nil {
		return "unknown", err
	}
	if reply.Error != "" {
		return reply.Status, fmt.Errorf("%s", reply.Error)
	}
	return reply.Status, nil
}
