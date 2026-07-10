package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iamkaf/pastel/internal/ui"
)

// Minecraft: [04:19:30] [Server thread/INFO]: Done (0.652s)! For help, type "help"
var serverDoneRE = regexp.MustCompile(`(?i)Done\s*\([^)]*\)!\s*For help`)

// Crash / hard-fail patterns (Fabric, Forge, generic).
var serverFailedRE = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`Failed to start the minecraft server`,
	`Could not execute entrypoint`,
	`Exception in server tick loop`,
	`Minecraft has crashed`,
	`#@!@# Game crashed`,
	`A fatal exception has occurred`,
	`Unable to begin loading`,
}, "|"))

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// waitForBoot shows a live boot UI until:
//   - the log shows Minecraft's "Done (…)! For help" line (and process still alive),
//   - the user presses Enter (detach early; server keeps booting), or
//   - the Java process exits / the log shows a fatal startup error.
//
// exited is closed/signaled when cmd.Wait returns (authoritative process death).
// captureLog is Pastel's stdout capture; latestLog is logs/latest.log (may appear mid-boot).
func waitForBoot(root string, pid int, captureLog, latestLog string, exited <-chan error) error {
	startCapture := logSize(captureLog)
	startLatest := logSize(latestLog) // often 0 until logger initializes
	if !stdinIsTTY() {
		return waitForBootHeadless(root, pid, captureLog, latestLog, startCapture, startLatest, exited, 10*time.Minute)
	}

	enterCh := make(chan struct{}, 1)
	go readEnter(enterCh)

	ui.Blank()
	ui.Step("Starting Minecraft…")
	fmt.Fprintln(ui.Out, "  "+ui.Dim("Press ")+ui.Blue("Enter")+ui.Dim(" to finish anytime — the server keeps booting."))
	ui.Blank()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	frame := 0
	statusWidth := 0

	for {
		select {
		case <-exited:
			clearStatusLine(statusWidth)
			return earlyExitError(root, captureLog, latestLog)
		case <-enterCh:
			clearStatusLine(statusWidth)
			// If it already died, report crash instead of "continuing".
			select {
			case <-exited:
				return earlyExitError(root, captureLog, latestLog)
			default:
			}
			if !serverProcessStillOurs(root, pid) {
				return earlyExitError(root, captureLog, latestLog)
			}
			ui.Detail("Continuing in the background…")
			return nil
		case <-ticker.C:
			// Fatal log lines (even before process fully reaps)
			if logContainsFailureSince(captureLog, startCapture) || logContainsFailureSince(latestLog, startLatest) {
				// Give Wait a moment to fire; then fail either way.
				select {
				case <-exited:
				case <-time.After(800 * time.Millisecond):
				}
				clearStatusLine(statusWidth)
				return earlyExitError(root, captureLog, latestLog)
			}
			if !serverProcessStillOurs(root, pid) {
				// Double-check via Wait channel (may race)
				select {
				case <-exited:
				case <-time.After(300 * time.Millisecond):
				}
				clearStatusLine(statusWidth)
				return earlyExitError(root, captureLog, latestLog)
			}
			// Only count "Done!" written after this boot started.
			if logContainsDoneSince(captureLog, startCapture) || logContainsDoneSince(latestLog, startLatest) {
				if err := confirmStillAlive(root, pid, captureLog, latestLog, exited); err != nil {
					clearStatusLine(statusWidth)
					return err
				}
				clearStatusLine(statusWidth)
				ui.OK("Minecraft is ready")
				return nil
			}
			last := lastNonEmptyLogLineSince(latestLog, startLatest)
			if last == "" {
				last = lastNonEmptyLogLineSince(captureLog, startCapture)
			}
			spin := spinnerFrames[frame%len(spinnerFrames)]
			frame++
			line := formatBootStatus(spin, last)
			statusWidth = printStatusLine(line, statusWidth)
		}
	}
}

func waitForBootHeadless(root string, pid int, captureLog, latestLog string, startCapture, startLatest int64, exited <-chan error, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			return earlyExitError(root, captureLog, latestLog)
		default:
		}
		if logContainsFailureSince(captureLog, startCapture) || logContainsFailureSince(latestLog, startLatest) {
			select {
			case <-exited:
			case <-time.After(800 * time.Millisecond):
			}
			return earlyExitError(root, captureLog, latestLog)
		}
		if !serverProcessStillOurs(root, pid) {
			return earlyExitError(root, captureLog, latestLog)
		}
		if logContainsDoneSince(captureLog, startCapture) || logContainsDoneSince(latestLog, startLatest) {
			return confirmStillAlive(root, pid, captureLog, latestLog, exited)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !serverProcessStillOurs(root, pid) {
		return earlyExitError(root, captureLog, latestLog)
	}
	return nil
}

// confirmStillAlive waits briefly after Done; many bad mods explode in the next second.
func confirmStillAlive(root string, pid int, captureLog, latestLog string, exited <-chan error) error {
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			return earlyExitError(root, captureLog, latestLog)
		default:
		}
		if logContainsFailureSince(captureLog, 0) || logContainsFailureSince(latestLog, 0) {
			select {
			case <-exited:
			case <-time.After(500 * time.Millisecond):
			}
			return earlyExitError(root, captureLog, latestLog)
		}
		if !serverProcessStillOurs(root, pid) {
			return earlyExitError(root, captureLog, latestLog)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// serverProcessStillOurs is true if pid is alive AND still looks like this server's Minecraft,
// or (if pid unknown) findServerProcesses still finds one. Avoids PID-reuse false "alive".
func serverProcessStillOurs(root string, pid int) bool {
	if pid > 0 {
		if !processAlive(pid) {
			return false
		}
		// Verify cmdline/cwd still match this server (PID may have been reused).
		abs, err := filepath.Abs(root)
		if err != nil {
			abs = root
		}
		cmd := readProcCmdline(pid)
		cwd := readProcCwd(pid)
		if cmd == "" && cwd == "" {
			// Non-Linux or unreadable — fall back to Signal(0) only.
			return true
		}
		return isMinecraftServerProcess(cmd, abs, cwd)
	}
	return len(findServerProcesses(root)) > 0
}

func readProcCmdline(pid int) string {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(raw) == 0 {
		return ""
	}
	return strings.ReplaceAll(string(raw), "\x00", " ")
}

func logSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func logContainsDoneSince(path string, startOff int64) bool {
	return logMatchesSince(path, startOff, serverDoneRE)
}

func logContainsFailureSince(path string, startOff int64) bool {
	return logMatchesSince(path, startOff, serverFailedRE)
}

func logMatchesSince(path string, startOff int64, re *regexp.Regexp) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return false
	}
	if startOff < 0 {
		startOff = 0
	}
	if int64(len(data)) <= startOff {
		return false
	}
	return re.Match(data[startOff:])
}

func lastNonEmptyLogLineSince(path string, startOff int64) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	if startOff < 0 {
		startOff = 0
	}
	if int64(len(data)) <= startOff {
		return ""
	}
	text := string(data[startOff:])
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		s := strings.TrimSpace(lines[i])
		if s != "" {
			return s
		}
	}
	return ""
}

func lastNonEmptyLogLine(path string) string {
	return lastNonEmptyLogLineSince(path, 0)
}

func readEnter(ch chan struct{}) {
	buf := make([]byte, 64)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		for i := 0; i < n; i++ {
			if buf[i] == '\n' || buf[i] == '\r' {
				select {
				case ch <- struct{}{}:
				default:
				}
				return
			}
		}
	}
}

func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func formatBootStatus(spin, last string) string {
	prefix := ui.Blue(spin) + " "
	if last == "" {
		return prefix + ui.Dim("waiting for log…")
	}
	return prefix + ui.Dim(truncateRunes(last, 96))
}

func truncateRunes(s string, max int) string {
	if max <= 1 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max < 2 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func printStatusLine(line string, prevWidth int) int {
	pad := 0
	vis := visibleLen(line)
	if prevWidth > vis {
		pad = prevWidth - vis
	}
	fmt.Fprintf(ui.Out, "\r%s%s", line, strings.Repeat(" ", pad))
	return vis
}

func clearStatusLine(width int) {
	if width < 1 {
		width = 80
	}
	fmt.Fprintf(ui.Out, "\r%s\r", strings.Repeat(" ", width))
}

func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		n++
	}
	return n
}
