//go:build !windows

package runtime

import (
	"os"
	"syscall"
)

func signalPID(pid int, sig syscall.Signal) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(sig)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// findServerProcessesWindows is only used on Windows; stub keeps shared callers compiling.
func findServerProcessesWindows(string) []ProcInfo { return nil }
