//go:build windows

package runtime

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const stillActive = 259

func signalPID(pid int, _ syscall.Signal) error {
	return terminateProcess(pid)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

func terminateProcess(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

// processCommandLine returns the full command line of pid, or false when it cannot be read.
func processCommandLine(pid int) (string, bool) {
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf(`(Get-CimInstance Win32_Process -Filter "ProcessId = %d").CommandLine`, pid)).Output()
	if err != nil {
		return "", false
	}
	cmd := strings.TrimSpace(string(out))
	return cmd, cmd != ""
}
