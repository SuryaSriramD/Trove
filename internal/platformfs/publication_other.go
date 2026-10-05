//go:build !darwin && !linux && !windows

package platformfs

func ensurePrivateDirectory(string) error    { return unsupported() }
func auditWorkspace(string) error            { return unsupported() }
func requireRecoveryDurability() error       { return ErrDirectoryDurability }
func syncDirectory(string) error             { return ErrDirectoryDurability }
func publishFile(string, string, bool) error { return unsupported() }
