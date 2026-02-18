//go:build darwin || linux

package nativeagent

import (
	"fmt"
	"os"
	"syscall"
)

type ProcessLock struct {
	file *os.File
}

func AcquireProcessLock(path string) (*ProcessLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("another proxer-agent instance is already running")
	}
	return &ProcessLock{file: file}, nil
}

func (l *ProcessLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		_ = l.file.Close()
		return err
	}
	return l.file.Close()
}
