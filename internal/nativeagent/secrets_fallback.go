package nativeagent

import "context"

type unsupportedSecretStore struct{}

func (s *unsupportedSecretStore) Set(ctx context.Context, key, value string) error {
	return ErrSecretUnavailable
}

func (s *unsupportedSecretStore) Get(ctx context.Context, key string) (string, error) {
	return "", ErrSecretUnavailable
}

func (s *unsupportedSecretStore) Delete(ctx context.Context, key string) error {
	return ErrSecretUnavailable
}
