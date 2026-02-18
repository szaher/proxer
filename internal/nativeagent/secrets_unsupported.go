//go:build !darwin && !linux && !windows

package nativeagent

func newPlatformSecretStore() SecretStore {
	return &unsupportedSecretStore{}
}
