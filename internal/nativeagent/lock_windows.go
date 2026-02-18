//go:build windows

package nativeagent

import (
	"fmt"
	"os"
)

type ProcessLock struct {
	file *os.File
}

func AcquireProcessLock(path string) (*ProcessLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another proxer-agent instance is already running")
		}
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	return &ProcessLock{file: file}, nil
}

func (l *ProcessLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	name := l.file.Name()
	if err := l.file.Close(); err != nil {
		return err
	}
	if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
