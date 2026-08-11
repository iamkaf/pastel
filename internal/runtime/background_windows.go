//go:build windows

package runtime

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"github.com/iamkaf/pastel/internal/state"
	"golang.org/x/sys/windows"
)

func supportsAttachedConsole() bool { return true }

func terminateSupervisor(pid int) error {
	return KillPID(pid)
}

// Supervise owns the background Java process on Windows. It joins a
// kill-on-close job before publishing its PID, so Java and any descendants die
// if the supervisor is terminated unexpectedly.
func Supervise(root, java string, args []string, autoRestart bool) error {
	job, err := createSupervisorJob()
	if err != nil {
		return fmt.Errorf("supervisor job: %w", err)
	}
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("join supervisor job: %w", err)
	}
	// The job handle intentionally remains open for the supervisor's lifetime.
	// Closing the last handle would terminate this process as a job member.
	if err := os.WriteFile(state.SupervisorPIDPath(root), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return err
	}

	err = superviseWindows(root, java, args, autoRestart)
	// Keep the raw handle visibly live until supervision ends. The operating
	// system closes it when this dedicated supervisor process exits.
	goruntime.KeepAlive(job)
	return err
}

func createSupervisorJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
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

// superviseWindows runs inside the dedicated supervisor process. Console
// commands arrive through an owner-restricted named pipe whose address is
// stored in console.in. An anonymous pipe feeds Java stdin so disconnecting
// pastel console does not EOF the server.
func superviseWindows(root, java string, args []string, autoRestart bool) error {
	ln, pipeName, err := listenConsolePipe()
	if err != nil {
		return fmt.Errorf("console listener: %w", err)
	}
	defer ln.Close()
	if err := os.WriteFile(state.ConsoleInPath(root), []byte("pipe:"+pipeName+"\n"), 0o644); err != nil {
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

func listenConsolePipe() (net.Listener, string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return nil, "", err
	}
	pipeName := `\\.\pipe\pastel-console-` + hex.EncodeToString(random)
	sddl, err := consolePipeSecurityDescriptor()
	if err != nil {
		return nil, "", err
	}
	ln, err := winio.ListenPipe(pipeName, &winio.PipeConfig{
		SecurityDescriptor: sddl,
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   64 * 1024,
	})
	if err != nil {
		return nil, "", err
	}
	return ln, pipeName, nil
}

func consolePipeSecurityDescriptor() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	sid := user.User.Sid.String()
	if sid == "" {
		return "", fmt.Errorf("current user has no security identifier")
	}
	// Protected DACL: deny network logons, then allow only SYSTEM,
	// administrators, and the Windows account that launched Pastel.
	return fmt.Sprintf("D:P(D;;GA;;;NU)(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;%s)", sid), nil
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
	pipeName := strings.TrimSpace(string(data))
	if !strings.HasPrefix(pipeName, "pipe:") {
		return fmt.Errorf("console not available (is the server running via ./pastel run?)")
	}
	pipeName = strings.TrimPrefix(pipeName, "pipe:")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	conn, err := winio.DialPipeContext(ctx, pipeName)
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
