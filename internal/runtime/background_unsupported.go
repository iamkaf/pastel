//go:build !darwin && !linux

package runtime

import "fmt"

func supportsAttachedConsole() bool { return false }

func startBackground(_ Options, _ string, _ []string) error {
	return fmt.Errorf("background servers are not available on this platform yet — use ./pastel run -f")
}

func HoldFIFO(_ string) error {
	return fmt.Errorf("the background console is not available on this platform")
}

func SendCommand(_ string, _ string) error {
	return fmt.Errorf("the attached console is not available on this platform — run the server with ./pastel run -f")
}
