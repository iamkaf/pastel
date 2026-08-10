//go:build windows

package runtime

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/iamkaf/pastel/internal/state"
	"golang.org/x/sys/windows"
)

func supportsAttachedConsole() bool { return true }

func terminateSupervisor(pid int) error {
	return KillPID(pid)
}

func startBackground(opt Options, java string, args []string) error {
	logPath := state.ConsoleLogPath(opt.Root)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	logFile.Close()

	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	supervisorArgs := []string{"__supervise", opt.Root, strconv.FormatBool(opt.AutoRestart), "--", java}
	supervisorArgs = append(supervisorArgs, args...)
	cmd := exec.Command(self, supervisorArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("couldn't start server supervisor: %w", err)
	}
	if err := os.WriteFile(state.SupervisorPIDPath(opt.Root), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		_ = cmd.Process.Kill()
		return err
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	serverPID, err := waitForServerPID(opt.Root, exited, 10*time.Second)
	if err != nil {
		cleanupServerFiles(opt.Root)
		return err
	}
	latestLog := filepath.Join(opt.Root, "logs", "latest.log")
	if err := waitForBoot(opt.Root, serverPID, logPath, latestLog, exited); err != nil {
		cleanupServerFiles(opt.Root)
		return err
	}

	printBackgroundReady()
	return nil
}

// Supervise owns the background Java process on Windows. Console commands arrive
// over a loopback TCP listener whose address is stored in console.in (FIFOs are
// not available). An anonymous pipe feeds Java stdin so disconnecting pastel
// console does not EOF the server.
func Supervise(root, java string, args []string, autoRestart bool) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("console listener: %w", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()
	if err := os.WriteFile(state.ConsoleInPath(root), []byte("tcp:"+addr+"\n"), 0o644); err != nil {
		return err
	}
	defer os.Remove(state.ConsoleInPath(root))

	feeder := &consoleFeeder{}
	go feeder.acceptLoop(ln)

	logPath := state.ConsoleLogPath(root)
	latestLog := filepath.Join(root, "logs", "latest.log")
	for {
		startConsole := logSize(logPath)
		startLatest := logSize(latestLog)

		stdinR, stdinW, err := os.Pipe()
		if err != nil {
			return fmt.Errorf("console pipe: %w", err)
		}
		feeder.setWriter(stdinW)

		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			feeder.setWriter(nil)
			stdinR.Close()
			return err
		}
		cmd := exec.Command(java, args...)
		cmd.Dir = root
		cmd.Stdin = stdinR
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
			HideWindow:    true,
		}
		if err := cmd.Start(); err != nil {
			feeder.setWriter(nil)
			stdinR.Close()
			logFile.Close()
			return fmt.Errorf("couldn't start Java (%s): %w", java, err)
		}
		stdinR.Close() // Java holds the read end
		if err := os.WriteFile(state.PIDPath(root), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
			_ = cmd.Process.Kill()
			feeder.setWriter(nil)
			logFile.Close()
			return err
		}

		err = cmd.Wait()
		feeder.setWriter(nil)
		_ = os.Remove(state.PIDPath(root))
		ready := logContainsDoneSince(logPath, startConsole) || logContainsDoneSince(latestLog, startLatest)
		if !shouldRestartServer(err, ready, autoRestart) {
			logFile.Close()
			cleanupServerFiles(root)
			return err
		}
		fmt.Fprintln(logFile, "[Pastel] Server crashed; restarting in 5 seconds…")
		logFile.Close()
		time.Sleep(5 * time.Second)
	}
}

type consoleFeeder struct {
	mu sync.Mutex
	w  *os.File
}

func (f *consoleFeeder) setWriter(w *os.File) {
	f.mu.Lock()
	old := f.w
	f.w = w
	f.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

func (f *consoleFeeder) writeLine(line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.w == nil {
		return fmt.Errorf("console not ready")
	}
	_, err := fmt.Fprintln(f.w, line)
	return err
}

func (f *consoleFeeder) acceptLoop(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go f.handleConn(c)
	}
}

func (f *consoleFeeder) handleConn(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if err := f.writeLine(line); err != nil {
			return
		}
	}
}

// HoldFIFO is unused on Windows (console uses TCP). Kept for CLI parity.
func HoldFIFO(_ string) error {
	return fmt.Errorf("console hold is not used on Windows")
}

func SendCommand(root, line string) error {
	data, err := os.ReadFile(state.ConsoleInPath(root))
	if err != nil {
		return fmt.Errorf("console not available (is the server running via ./pastel run?)")
	}
	addr := strings.TrimSpace(string(data))
	if !strings.HasPrefix(addr, "tcp:") {
		return fmt.Errorf("console not available (is the server running via ./pastel run?)")
	}
	addr = strings.TrimPrefix(addr, "tcp:")
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("open console: %w", err)
	}
	defer conn.Close()
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	_ = conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
	_, err = fmt.Fprintln(conn, line)
	return err
}
