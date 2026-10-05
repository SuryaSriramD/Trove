// trove-format emits the workspace compatibility advertised by this source.
package main

import (
	"encoding/json"
	"os"
	"trove.local/phase0/internal/workspace"
)

func main() {
	if err := json.NewEncoder(os.Stdout).Encode(workspace.CurrentCompatibility()); err != nil {
		os.Exit(1)
	}
}
