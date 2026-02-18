//go:build linux

package nativeagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ConfigureStartAtLogin(enabled bool, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".config", "autostart")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	desktopPath := filepath.Join(dir, "proxer-agent.desktop")
	if !enabled {
		if err := os.Remove(desktopPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	quotedArgs := []string{shellQuote(executable)}
	for _, arg := range args {
		if strings.TrimSpace(arg) != "" {
			quotedArgs = append(quotedArgs, shellQuote(arg))
		}
	}
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Version=1.0
Name=Proxer Agent
Comment=Proxer Native Agent
Exec=%s
X-GNOME-Autostart-enabled=true
`, strings.Join(quotedArgs, " "))

	if err := os.WriteFile(desktopPath, []byte(content), 0o644); err != nil {
		return err
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
