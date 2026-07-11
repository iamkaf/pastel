//go:build darwin || linux

package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/iamkaf/pastel/internal/state"
	"github.com/iamkaf/pastel/internal/ui"
)

func supportsAttachedConsole() bool { return true }

func terminateSupervisor(pid int) error {
	// The supervisor starts a new session and Java inherits its process group.
	// Signal the group so an aborted startup cannot leave Java orphaned.
	return syscall.Kill(-pid, syscall.SIGTERM)
}

func startBackground(opt Options, java string, args []string) error {
	fifo := state.ConsoleInPath(opt.Root)
	logPath := state.ConsoleLogPath(opt.Root)
	_ = os.Remove(fifo)
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		return fmt.Errorf("console fifo: %w", err)
	}

	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	hold := exec.Command(self, "__hold-fifo", fifo)
	hold.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := hold.Start(); err != nil {
		return fmt.Errorf("console hold: %w", err)
	}
	_ = os.WriteFile(state.HoldPIDPath(opt.Root), []byte(strconv.Itoa(hold.Process.Pid)+"\n"), 0o644)
	_ = hold.Process.Release()

	time.Sleep(100 * time.Millisecond)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		stopHold(opt.Root)
		return err
	}
	logFile.Close()

	supervisorArgs := []string{"__supervise", opt.Root, "--", java}
	supervisorArgs = append(supervisorArgs, args...)
	cmd := exec.Command(self, supervisorArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		stopHold(opt.Root)
		return fmt.Errorf("couldn't start server supervisor: %w", err)
	}
	if err := os.WriteFile(state.SupervisorPIDPath(opt.Root), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		_ = cmd.Process.Kill()
		stopHold(opt.Root)
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

	ui.BigOK("Your server is running in the background")
	ui.Title("What you can do now")
	fmt.Fprintf(ui.Out, "  %s  %s\n", ui.Blue(padRunCmd("./pastel console")), "See the live log and type commands")
	fmt.Fprintf(ui.Out, "  %s  %s\n", ui.Blue(padRunCmd("./pastel stop")), "Shut the server down when you're done")
	fmt.Fprintf(ui.Out, "  %s  %s\n", ui.Blue(padRunCmd("./pastel")), "Check status anytime")
	ui.Blank()
	ui.Detail("You can close this terminal — the server keeps going.")
	return nil
}

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

// Supervise owns the background Java process. A server that has reached the
// Minecraft ready message is restarted after a non-zero exit; clean shutdowns
// and startup failures remain stopped.
func Supervise(root, java string, args []string) error {
	logPath := state.ConsoleLogPath(root)
	latestLog := filepath.Join(root, "logs", "latest.log")
	for {
		startConsole := logSize(logPath)
		startLatest := logSize(latestLog)
		stdin, err := os.OpenFile(state.ConsoleInPath(root), os.O_RDONLY, 0)
		if err != nil {
			return fmt.Errorf("open console fifo for server: %w", err)
		}
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			stdin.Close()
			return err
		}
		cmd := exec.Command(java, args...)
		cmd.Dir = root
		cmd.Stdin = stdin
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		if err := cmd.Start(); err != nil {
			stdin.Close()
			logFile.Close()
			return fmt.Errorf("couldn't start Java (%s): %w", java, err)
		}
		if err := os.WriteFile(state.PIDPath(root), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
			_ = cmd.Process.Kill()
			stdin.Close()
			logFile.Close()
			return err
		}
		err = cmd.Wait()
		stdin.Close()
		_ = os.Remove(state.PIDPath(root))
		ready := logContainsDoneSince(logPath, startConsole) || logContainsDoneSince(latestLog, startLatest)
		if !shouldRestartServer(err, ready) {
			logFile.Close()
			cleanupServerFiles(root)
			return err
		}
		fmt.Fprintln(logFile, "[Pastel] Server crashed; restarting in 5 seconds…")
		logFile.Close()
		time.Sleep(5 * time.Second)
	}
}

func shouldRestartServer(waitErr error, reachedReady bool) bool {
	if !reachedReady || waitErr == nil {
		return false
	}
	exit, ok := waitErr.(*exec.ExitError)
	return ok && exit.ExitCode() != 0
}

func HoldFIFO(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	return nil
}

func SendCommand(root, line string) error {
	fifo := state.ConsoleInPath(root)
	if _, err := os.Stat(fifo); err != nil {
		return fmt.Errorf("console not available (is the server running via ./pastel run?)")
	}
	f, err := openFIFOWriteTimeout(fifo, 500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("open console: %w", err)
	}
	defer f.Close()
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	_ = f.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
	_, err = fmt.Fprintln(f, line)
	return err
}

func openFIFOWriteTimeout(path string, timeout time.Duration) (*os.File, error) {
	type result struct {
		f   *os.File
		err error
	}
	ch := make(chan result, 1)
	go func() {
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			ch <- result{err: err}
			return
		}
		if err := syscall.SetNonblock(fd, false); err != nil {
			_ = syscall.Close(fd)
			ch <- result{err: err}
			return
		}
		ch <- result{f: os.NewFile(uintptr(fd), path)}
	}()
	select {
	case r := <-ch:
		return r.f, r.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("console not ready (timed out — is the server still up?)")
	}
}
