//go:build windows

package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iamkaf/pastel/internal/state"
)

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
		errCh <- Supervise(root, java, args, false)
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

func TestCommandContainsPathWindows(t *testing.T) {
	cmd := `C:\Java\bin\java.exe -jar C:\Servers\MyPack\fabric-server-mc.jar nogui`
	if !commandContainsPath(cmd, `C:\Servers\MyPack`) {
		t.Fatal("expected match")
	}
	if !commandContainsPath(cmd, `c:\servers\mypack`) {
		t.Fatal("expected case-insensitive match")
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
