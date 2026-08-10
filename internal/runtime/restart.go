package runtime

import "os/exec"

// shouldRestartServer reports whether a background supervisor should relaunch Java.
// Clean shutdowns (nil wait error) and startup failures (never reached ready) stay stopped.
func shouldRestartServer(waitErr error, reachedReady, autoRestart bool) bool {
	if !autoRestart || !reachedReady || waitErr == nil {
		return false
	}
	exit, ok := waitErr.(*exec.ExitError)
	return ok && exit.ExitCode() != 0
}
