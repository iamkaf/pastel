//go:build windows

package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iamkaf/pastel/internal/state"
)

const supervisorTestRootEnv = "PASTEL_TEST_SUPERVISOR_ROOT"
const supervisorTestModeEnv = "PASTEL_TEST_SUPERVISOR_MODE"

func TestWindowsSupervisorProcess(t *testing.T) {
	root := os.Getenv(supervisorTestRootEnv)
	if root == "" {
		return
	}
	mode := os.Getenv(supervisorTestModeEnv)
	var ps string
	switch mode {
	case "wait":
		ps = `Write-Output '[00:00:00] [Server thread/INFO]: Done (0.1s)! For help, type "help"'; $r = New-Object System.IO.StreamReader([Console]::OpenStandardInput()); while (($line = $r.ReadLine()) -ne $null) { if ($line -eq 'stop') { break } }`
	case "crash":
		ps = `Write-Output '[00:00:00] [Server thread/INFO]: Done (0.1s)! For help, type "help"'; exit 7`
	default:
		t.Fatalf("unknown supervisor test mode %q", mode)
	}
	if err := Supervise(root, "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", ps}, true); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsBackgroundFakeServer(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}

	// Use powershell.exe (already trusted by App Control) as a fake Minecraft
	// process that prints Done and exits on "stop".
	ps := `Write-Output '[00:00:00] [Server thread/INFO]: Done (0.1s)! For help, type "help"'; $r = New-Object System.IO.StreamReader([Console]::OpenStandardInput()); while (($line = $r.ReadLine()) -ne $null) { if ($line -eq 'stop') { break } }`
	java := "powershell"
	args := []string{"-NoProfile", "-NonInteractive", "-Command", ps}

	errCh := make(chan error, 1)
	go func() {
		errCh <- superviseWindows(root, java, args, false)
	}()

	deadline := time.Now().Add(15 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("supervise exited early: %v", err)
		default:
		}
		if p, ok := readAlivePID(state.PIDPath(root)); ok {
			pid = p
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("java pid never appeared")
	}

	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if logContainsDoneSince(state.ConsoleLogPath(root), 0) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !logContainsDoneSince(state.ConsoleLogPath(root), 0) {
		t.Fatalf("never saw Done in console.log; contents=%q", readLogTail(state.ConsoleLogPath(root), 4096))
	}

	if err := SendCommand(root, "stop"); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("fake server did not exit after stop")
}

func TestWindowsCleanupTerminatesSupervisorJob(t *testing.T) {
	root := newWindowsTestRoot(t)
	cmd := startWindowsSupervisorProcess(t, root, "wait")
	serverPID := waitForWindowsTestPID(t, state.PIDPath(root), 15*time.Second)

	cleanupServerFiles(root)
	waitForWindowsTestProcessExit(t, cmd, 10*time.Second)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(serverPID) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server process %d survived supervisor cleanup", serverPID)
}

func TestWindowsStopDuringRestartDelayStopsSupervisor(t *testing.T) {
	root := newWindowsTestRoot(t)
	cmd := startWindowsSupervisorProcess(t, root, "crash")
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(readLogTail(state.ConsoleLogPath(root), 4096), "restarting in 5 seconds") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(readLogTail(state.ConsoleLogPath(root), 4096), "restarting in 5 seconds") {
		t.Fatal("supervisor never entered the restart delay")
	}
	if _, alive := readAlivePID(state.PIDPath(root)); alive {
		t.Fatal("server must be absent during the restart delay")
	}

	if err := Stop(root); err != nil {
		t.Fatal(err)
	}
	waitForWindowsTestProcessExit(t, cmd, 10*time.Second)
	if _, alive := readAlivePID(state.SupervisorPIDPath(root)); alive {
		t.Fatal("supervisor survived stop during restart delay")
	}
	time.Sleep(5500 * time.Millisecond)
	if _, alive := readAlivePID(state.PIDPath(root)); alive {
		t.Fatal("server restarted after stop returned")
	}
	log := readLogTail(state.ConsoleLogPath(root), 16*1024)
	if launches := strings.Count(log, "Done (0.1s)!"); launches != 1 {
		t.Fatalf("server launched %d times after stop; want exactly once", launches)
	}
}

func newWindowsTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func startWindowsSupervisorProcess(t *testing.T, root, mode string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsSupervisorProcess$")
	cmd.Env = append(os.Environ(), supervisorTestRootEnv+"="+root, supervisorTestModeEnv+"="+mode)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
	})
	waitForWindowsTestPID(t, state.SupervisorPIDPath(root), 10*time.Second)
	return cmd
}

func waitForWindowsTestPID(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pid, ok := readAlivePID(path); ok {
			return pid
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("live pid never appeared at %s", filepath.Base(path))
	return 0
}

func waitForWindowsTestProcessExit(t *testing.T, cmd *exec.Cmd, timeout time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("process %d did not exit", cmd.Process.Pid)
	}
}

func TestCommandContainsPathWindows(t *testing.T) {
	cmd := `C:\Java\bin\java.exe -jar C:\Servers\MyPack\fabric-server-mc.jar nogui`
	if !commandContainsPath(cmd, `C:\Servers\MyPack`) {
		t.Fatal("expected match")
	}
	if !commandContainsPath(cmd, `c:\servers\mypack`) {
		t.Fatal("expected case-insensitive match")
	}
	if commandContainsPath(cmd, `C:\Servers\My`) {
		t.Fatal("path prefix must not match a different server root")
	}
	if commandContainsPath(`java -jar C:\Servers\MyPack-Dev\server.jar nogui`, `C:\Servers\MyPack`) {
		t.Fatal("sibling server path must not match")
	}
}

func TestConsolePipeSecurityDescriptorIsOwnerRestricted(t *testing.T) {
	sddl, err := consolePipeSecurityDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sddl, ";;;WD)") || strings.Contains(sddl, ";;;AN)") {
		t.Fatal("descriptor grants broad access")
	}
	if !strings.Contains(sddl, "(D;;GA;;;NU)") {
		t.Fatal("descriptor does not deny network logons")
	}
}

func TestProcessAliveSelf(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("current process should be alive")
	}
	if processAlive(99999999) {
		t.Fatal("bogus pid should be dead")
	}
}
