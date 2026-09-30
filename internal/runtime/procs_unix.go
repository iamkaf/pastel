//go:build !windows

package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

// processCommandLine returns the full command line of pid, or false when it cannot be read.
func processCommandLine(pid int) (string, bool) {
	if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil && len(raw) > 0 {
		return strings.TrimSpace(strings.ReplaceAll(string(raw), "\x00", " ")), true
	}
	// macOS has no /proc; -ww keeps ps from truncating long Java command lines.
	out, err := exec.Command("ps", "-ww", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", false
	}
	cmd := strings.TrimSpace(string(out))
	return cmd, cmd != ""
}
