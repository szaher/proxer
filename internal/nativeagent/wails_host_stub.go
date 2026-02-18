//go:build !wails

package nativeagent

import "context"

func runWailsHost(ctx context.Context) error {
	return ErrWailsUnavailable
}
