package nativeagent

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrSecretNotFound    = errors.New("secret not found")
	ErrSecretUnavailable = errors.New("secret store unavailable")
)

type SecretStore interface {
	Set(ctx context.Context, key, value string) error
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
}

func NewSecretStore() SecretStore {
	return newPlatformSecretStore()
}

func secretKeyForProfile(profileID, field string) string {
	return fmt.Sprintf("profile/%s/%s", profileID, field)
}
