//go:build darwin

package nativeagent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const darwinLaunchAgentID = "io.proxer.agent"

func ConfigureStartAtLogin(enabled bool, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	launchAgentsDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgentsDir, 0o755); err != nil {
		return err
	}
	plistPath := filepath.Join(launchAgentsDir, darwinLaunchAgentID+".plist")
	if !enabled {
		_ = exec.Command("launchctl", "unload", plistPath).Run()
		if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	programArgs := []string{executable}
	for _, arg := range args {
		if strings.TrimSpace(arg) != "" {
			programArgs = append(programArgs, arg)
		}
	}

	var argItems strings.Builder
	for _, arg := range programArgs {
		argItems.WriteString("    <string>")
		argItems.WriteString(xmlEscape(arg))
		argItems.WriteString("</string>\n")
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
%s  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <false/>
</dict>
</plist>
`, darwinLaunchAgentID, argItems.String())

	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}
	if output, err := exec.Command("launchctl", "load", "-w", plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("load launch agent: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func xmlEscape(input string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(input)
}
