//go:build darwin

package nativeagent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	macOSSecretAccount = "proxer-agent"
	macOSSecretService = "io.proxer.agent"
)

type macOSSecretStore struct{}

func newPlatformSecretStore() SecretStore {
	return &macOSSecretStore{}
}

func (s *macOSSecretStore) Set(ctx context.Context, key, value string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("secret key is required")
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("secret value is required")
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security", "add-generic-password",
		"-a", macOSSecretAccount,
		"-s", macOSSecretService+":"+key,
		"-w", value,
		"-U",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("persist secret in macOS keychain: %w (%s)", classifySecretError(err), strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *macOSSecretStore) Get(ctx context.Context, key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("secret key is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security", "find-generic-password",
		"-a", macOSSecretAccount,
		"-s", macOSSecretService+":"+key,
		"-w",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if bytes.Contains(output, []byte("could not be found")) {
			return "", ErrSecretNotFound
		}
		return "", fmt.Errorf("read secret from macOS keychain: %w (%s)", classifySecretError(err), strings.TrimSpace(string(output)))
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", ErrSecretNotFound
	}
	return value, nil
}

func (s *macOSSecretStore) Delete(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("secret key is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security", "delete-generic-password",
		"-a", macOSSecretAccount,
		"-s", macOSSecretService+":"+key,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if bytes.Contains(output, []byte("could not be found")) {
			return nil
		}
		return fmt.Errorf("delete secret from macOS keychain: %w (%s)", classifySecretError(err), strings.TrimSpace(string(output)))
	}
	return nil
}

func classifySecretError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*exec.Error); ok {
		return ErrSecretUnavailable
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 0 {
			return ErrSecretUnavailable
		}
	}
	return err
}
