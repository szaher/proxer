//go:build linux

package nativeagent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type linuxSecretStore struct{}

func newPlatformSecretStore() SecretStore {
	return &linuxSecretStore{}
}

func (s *linuxSecretStore) Set(ctx context.Context, key, value string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("secret key is required")
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("secret value is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "secret-tool", "store", "--label", "proxer-agent", "service", "proxer-agent", "key", key)
	cmd.Stdin = strings.NewReader(value)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("persist secret in linux keyring: %w (%s)", ErrSecretUnavailable, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *linuxSecretStore) Get(ctx context.Context, key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("secret key is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "secret-tool", "lookup", "service", "proxer-agent", "key", key)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(strings.TrimSpace(string(output))) == 0 {
			return "", ErrSecretNotFound
		}
		return "", fmt.Errorf("read secret from linux keyring: %w (%s)", ErrSecretUnavailable, strings.TrimSpace(string(output)))
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", ErrSecretNotFound
	}
	return value, nil
}

func (s *linuxSecretStore) Delete(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("secret key is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "secret-tool", "clear", "service", "proxer-agent", "key", key)
	if output, err := cmd.CombinedOutput(); err != nil {
		if len(strings.TrimSpace(string(output))) == 0 {
			return nil
		}
		return fmt.Errorf("delete secret from linux keyring: %w (%s)", ErrSecretUnavailable, strings.TrimSpace(string(output)))
	}
	return nil
}
