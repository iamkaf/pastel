package runtime

import (
	"fmt"
	"time"

	"github.com/iamkaf/pastel/internal/state"
)

func waitForServerPID(root string, exited <-chan error, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			if err != nil {
				return 0, fmt.Errorf("server supervisor exited: %w", err)
			}
			return 0, fmt.Errorf("server supervisor exited before Java started")
		default:
		}
		if pid, ok := readAlivePID(state.PIDPath(root)); ok {
			return pid, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, fmt.Errorf("timed out waiting for Java to start")
}
