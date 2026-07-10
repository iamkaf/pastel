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
	stdin, err := os.OpenFile(fifo, os.O_RDONLY, 0)
	if err != nil {
		stopHold(opt.Root)
		return fmt.Errorf("open console fifo for server: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		stdin.Close()
		stopHold(opt.Root)
		return err
	}

	cmd := exec.Command(java, args...)
	cmd.Dir = opt.Root
	cmd.Stdin = stdin
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		logFile.Close()
		stopHold(opt.Root)
		return fmt.Errorf("couldn't start Java (%s): %w", java, err)
	}
	serverPID := cmd.Process.Pid
	stdin.Close()
	logFile.Close()
	if serverPID <= 0 {
		time.Sleep(200 * time.Millisecond)
		if found := findServerProcesses(opt.Root); len(found) > 0 {
			serverPID = found[0]
		}
	}
	if serverPID > 0 {
		if err := os.WriteFile(state.PIDPath(opt.Root), []byte(strconv.Itoa(serverPID)+"\n"), 0o644); err != nil {
			_ = cmd.Process.Kill()
			stopHold(opt.Root)
			return err
		}
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
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
