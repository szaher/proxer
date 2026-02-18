//go:build windows

package nativeagent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type windowsSecretStore struct {
	dir string
}

func newPlatformSecretStore() SecretStore {
	dir, err := ConfigDir()
	if err != nil {
		return &unsupportedSecretStore{}
	}
	return &windowsSecretStore{dir: filepath.Join(dir, "secrets")}
}

func (s *windowsSecretStore) secretPath(key string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return filepath.Join(s.dir, replacer.Replace(key)+".txt")
}

func (s *windowsSecretStore) Set(ctx context.Context, key, value string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("secret key is required")
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("secret value is required")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create secret directory: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"$v = ConvertTo-SecureString -String $env:PROXER_SECRET_VALUE -AsPlainText -Force; $e = ConvertFrom-SecureString -SecureString $v; Set-Content -Path $env:PROXER_SECRET_PATH -Value $e -NoNewline")
	cmd.Env = append(os.Environ(),
		"PROXER_SECRET_VALUE="+value,
		"PROXER_SECRET_PATH="+s.secretPath(key),
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("persist secret in windows dpapi: %w (%s)", ErrSecretUnavailable, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *windowsSecretStore) Get(ctx context.Context, key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("secret key is required")
	}
	secretPath := s.secretPath(key)
	if _, err := os.Stat(secretPath); err != nil {
		if os.IsNotExist(err) {
			return "", ErrSecretNotFound
		}
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"$e = Get-Content -Path $env:PROXER_SECRET_PATH -Raw; if ([string]::IsNullOrWhiteSpace($e)) { exit 3 }; $s = ConvertTo-SecureString -String $e; $b = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($s); [Runtime.InteropServices.Marshal]::PtrToStringBSTR($b)")
	cmd.Env = append(os.Environ(), "PROXER_SECRET_PATH="+secretPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("read secret from windows dpapi: %w (%s)", ErrSecretUnavailable, strings.TrimSpace(string(output)))
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", ErrSecretNotFound
	}
	return value, nil
}

func (s *windowsSecretStore) Delete(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("secret key is required")
	}
	secretPath := s.secretPath(key)
	if err := os.Remove(secretPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
