//go:build !darwin || !cgo

package auth

func nativeSecretStore(string) (SecretStore, error) { return nil, ErrUnsupported }
