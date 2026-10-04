// Package sqlitevec registers the statically linked, pinned sqlite-vec runtime.
// This adapter is original Trove code; the adjacent C runtime is upstream MIT.
package sqlitevec

/*
#cgo CFLAGS: -DSQLITE_CORE
#cgo linux LDFLAGS: -lm
#include "sqlite-vec.h"

static int trove_register_vec(void) {
    return sqlite3_auto_extension((void (*)(void))sqlite3_vec_init);
}
*/
import "C"

import "fmt"

// Auto registers the built-in extension for subsequently opened connections.
// It must run before workspace SQLite connections are created. No shared
// extension file is loaded, and no sqlite-vec database server is required.
func Auto() {
	if result := C.trove_register_vec(); result != C.SQLITE_OK {
		panic(fmt.Sprintf("cannot register bundled sqlite-vec: sqlite code %d", int(result)))
	}
}
