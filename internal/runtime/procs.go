package runtime

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ProcInfo is a discovered Java / Minecraft process.
type ProcInfo struct {
	PID     int
	Cmdline string
	Cwd     string // may end with " (deleted)" if the folder was removed
}

// findServerProcesses returns PIDs of Java processes that look like this server's
// dedicated Minecraft process (cwd or cmdline matches the server root).
func findServerProcesses(root string) []int {
	var pids []int
	for _, p := range findServerProcessInfos(root) {
		pids = append(pids, p.PID)
	}
	return pids
}

// findServerProcessInfos is like findServerProcesses but keeps cwd/cmdline for UX.
func findServerProcessInfos(root string) []ProcInfo {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	abs = filepath.Clean(abs)

	switch runtime.GOOS {
	case "linux":
		return findServerProcessesLinux(abs)
	default:
		return findServerProcessesPS(abs)
	}
}

func findServerProcessesLinux(absRoot string) []ProcInfo {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []ProcInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 {
			continue
		}
		raw, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil || len(raw) == 0 {
			continue
		}
		cmd := string(bytes.ReplaceAll(raw, []byte{0}, []byte{' '}))
		cwd := readProcCwd(pid)
		if isMinecraftServerProcess(cmd, absRoot, cwd) {
			out = append(out, ProcInfo{PID: pid, Cmdline: strings.TrimSpace(cmd), Cwd: cwd})
		}
	}
	return out
}

func findServerProcessesPS(absRoot string) []ProcInfo {
	// Best-effort: cmdline only (no portable cwd without lsof).
	outCmd, err := exec.Command("ps", "-ax", "-o", "pid=,command=").Output()
	if err != nil {
		return nil
	}
	var out []ProcInfo
	for _, line := range strings.Split(string(outCmd), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 1 {
			continue
		}
		cmd := strings.Join(fields[1:], " ")
		if isMinecraftServerProcess(cmd, absRoot, "") {
			out = append(out, ProcInfo{PID: pid, Cmdline: cmd})
		}
	}
	return out
}

// FindOrphanMinecraftServers finds Minecraft-looking Java processes whose
// working directory was deleted (common after rm -rf while the server ran),
// or that match a previous server path the user no longer has files for.
func FindOrphanMinecraftServers() []ProcInfo {
	if runtime.GOOS != "linux" {
		return nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []ProcInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 {
			continue
		}
		raw, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil || len(raw) == 0 {
			continue
		}
		cmd := string(bytes.ReplaceAll(raw, []byte{0}, []byte{' '}))
		if !looksLikeMinecraftServerCmd(cmd) {
			continue
		}
		cwd := readProcCwd(pid)
		// Deleted folder, or still "minecraft-ish" with no live root tracking.
		if strings.Contains(cwd, "(deleted)") || cwd == "" {
			out = append(out, ProcInfo{PID: pid, Cmdline: strings.TrimSpace(cmd), Cwd: cwd})
		}
	}
	return out
}

func readProcCwd(pid int) string {
	target, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	if err != nil {
		return ""
	}
	return target
}

// isMinecraftServerProcess matches a dedicated server for absRoot.
// Uses cwd (including "path (deleted)") because jar args are often relative
// and do not include the absolute server path on the command line.
func isMinecraftServerProcess(cmd, absRoot, cwd string) bool {
	if !looksLikeMinecraftServerCmd(cmd) {
		return false
	}
	if absRoot != "" && strings.Contains(cmd, absRoot) {
		return true
	}
	if absRoot != "" && cwdMatchesRoot(cwd, absRoot) {
		return true
	}
	return false
}

func looksLikeMinecraftServerCmd(cmd string) bool {
	if !strings.Contains(cmd, "java") {
		return false
	}
	if strings.Contains(cmd, "__hold-fifo") {
		return false
	}
	// Exclude common non-server Java (IDEs, Gradle, etc.)
	low := strings.ToLower(cmd)
	if strings.Contains(low, "gradle") || strings.Contains(low, "jdt.ls") ||
		strings.Contains(low, "language server") || strings.Contains(low, "intellij") {
		return false
	}
	// Dedicated server shapes
	if strings.Contains(cmd, "fabric-server-") || strings.Contains(cmd, "quilt-server-") {
		return true
	}
	if strings.Contains(cmd, "unix_args.txt") || strings.Contains(cmd, "win_args.txt") {
		return true
	}
	if strings.Contains(cmd, "-jar") && (strings.Contains(cmd, "nogui") || strings.Contains(cmd, "server.jar")) {
		return true
	}
	if strings.Contains(cmd, "neoforge") && strings.Contains(cmd, "@") {
		return true
	}
	return false
}

func cwdMatchesRoot(cwd, absRoot string) bool {
	if cwd == "" || absRoot == "" {
		return false
	}
	// Linux shows " /path (deleted)" when the directory was removed under the process.
	cleaned := strings.TrimSpace(cwd)
	cleaned = strings.TrimSuffix(cleaned, " (deleted)")
	cleaned = filepath.Clean(cleaned)
	return cleaned == filepath.Clean(absRoot)
}

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

// KillPID sends SIGTERM then SIGKILL to a single process (and is used by stop -pid).
func KillPID(pid int) error {
	if pid <= 1 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	if !processAlive(pid) {
		return fmt.Errorf("process %d is not running", pid)
	}
	_ = signalPID(pid, syscall.SIGTERM)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(pid) {
		_ = signalPID(pid, syscall.SIGKILL)
	}
	return nil
}
