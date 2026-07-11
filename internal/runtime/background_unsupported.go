//go:build !darwin && !linux

package runtime

import (
	"fmt"
	"syscall"
)

func supportsAttachedConsole() bool { return false }

func terminateSupervisor(pid int) error { return signalPID(pid, syscall.SIGTERM) }

func startBackground(_ Options, _ string, _ []string) error {
	return fmt.Errorf("background servers are not available on this platform yet — use ./pastel run -f")
}

func Supervise(_ string, _ string, _ []string, _ bool) error {
	return fmt.Errorf("background server supervision is not available on this platform")
}

func HoldFIFO(_ string) error {
	return fmt.Errorf("the background console is not available on this platform")
}

func SendCommand(_ string, _ string) error {
	return fmt.Errorf("the attached console is not available on this platform — run the server with ./pastel run -f")
}
