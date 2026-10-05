//go:build (!darwin && !linux && !windows) || (darwin && !cgo)

package auth

func nativeSecretStore(string) (SecretStore, error) { return nil, ErrUnsupported }
