package nativeagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

var ErrWailsUnavailable = errors.New("wails host shell is not compiled in this build")

func RunGUI(ctx context.Context) error {
	shell := strings.ToLower(strings.TrimSpace(os.Getenv("PROXER_AGENT_GUI_SHELL")))
	switch shell {
	case "", "auto":
		if err := runWailsHost(ctx); err != nil {
			if errors.Is(err, ErrWailsUnavailable) {
				return runBrowserGUI(ctx)
			}
			return err
		}
		return nil
	case "wails":
		if err := runWailsHost(ctx); err != nil {
			if errors.Is(err, ErrWailsUnavailable) {
				return fmt.Errorf("wails shell requested but unavailable; rebuild with -tags wails")
			}
			return err
		}
		return nil
	case "browser":
		return runBrowserGUI(ctx)
	default:
		return fmt.Errorf("unsupported PROXER_AGENT_GUI_SHELL=%q; supported: auto|wails|browser", shell)
	}
}
