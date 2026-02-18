//go:build windows

package nativeagent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func ConfigureStartAtLogin(enabled bool, args []string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := `"` + executable + `"`
	for _, arg := range args {
		if strings.TrimSpace(arg) != "" {
			command += ` "` + arg + `"`
		}
	}
	if enabled {
		cmd := exec.Command("reg", "add", `HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run`, "/v", "ProxerAgent", "/t", "REG_SZ", "/d", command, "/f")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("enable startup entry: %w (%s)", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	cmd := exec.Command("reg", "delete", `HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run`, "/v", "ProxerAgent", "/f")
	if output, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(strings.ToLower(string(output)), "unable to find") {
			return nil
		}
		return fmt.Errorf("disable startup entry: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}
