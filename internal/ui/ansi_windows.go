//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableWindowsANSI turns on VT processing so pastel RGB colors work in conhost/Windows Terminal.
func enableWindowsANSI() {
	for _, h := range []windows.Handle{windows.Stdout, windows.Stderr} {
		var mode uint32
		if err := windows.GetConsoleMode(h, &mode); err != nil {
			continue
		}
		mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		_ = windows.SetConsoleMode(h, mode)
	}
	// Keep os import used if build tags change; touch Stdout.
	_ = os.Stdout
}
