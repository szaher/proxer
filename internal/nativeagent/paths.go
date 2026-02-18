package nativeagent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	appDirName       = "proxer-agent"
	settingsFileName = "settings.json"
	statusFileName   = "status.json"
	logFileName      = "agent.log"
	lockFileName     = "app.lock"
)

func ConfigDir() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("PROXER_AGENT_CONFIG_DIR")); custom != "" {
		if err := os.MkdirAll(custom, 0o700); err != nil {
			return "", fmt.Errorf("create PROXER_AGENT_CONFIG_DIR: %w", err)
		}
		return custom, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	var dir string
	switch runtime.GOOS {
	case "darwin":
		dir = filepath.Join(home, "Library", "Application Support", appDirName)
	case "windows":
		if appData := strings.TrimSpace(os.Getenv("AppData")); appData != "" {
			dir = filepath.Join(appData, "ProxerAgent")
		} else {
			dir = filepath.Join(home, "AppData", "Roaming", "ProxerAgent")
		}
	default:
		if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
			dir = filepath.Join(xdg, appDirName)
		} else {
			dir = filepath.Join(home, ".config", appDirName)
		}
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		fallbackDir := filepath.Join(os.TempDir(), appDirName)
		if mkErr := os.MkdirAll(fallbackDir, 0o700); mkErr != nil {
			return "", fmt.Errorf("create config dir: %w", err)
		}
		return fallbackDir, nil
	}
	return dir, nil
}

func SettingsPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, settingsFileName), nil
}

func StatusPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, statusFileName), nil
}

func LogPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, logFileName), nil
}

func LockPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, lockFileName), nil
}
