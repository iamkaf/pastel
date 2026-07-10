package runtime

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/iamkaf/pastel/internal/state"
	"github.com/iamkaf/pastel/internal/ui"
)

// Attach opens the live server console (logs + command input).
// Exit with Ctrl+C / Ctrl+D — the server keeps running if still alive.
// If the server process dies while attached, the console exits automatically.
func Attach(root string) error {
	if !supportsAttachedConsole() {
		return fmt.Errorf("the attached console is not available on this platform — run the server with ./pastel run -f")
	}
	_, alive, err := Running(root)
	if err != nil {
		return err
	}
	if !alive {
		return fmt.Errorf("server is not running — start it with ./pastel run, then ./pastel console")
	}

	consoleLog := state.ConsoleLogPath(root)
	if _, err := os.Stat(consoleLog); err != nil {
		_ = os.WriteFile(consoleLog, nil, 0o644)
	}

	fmt.Fprintln(ui.Out, ui.Brand()+ui.Dim("  ·  server console"))
	ui.Blank()

	ui.Detail("——— recent log ———")
	var offset atomic.Int64
	if lines, off, err := tailLines(consoleLog, 30); err == nil {
		if lines != "" {
			fmt.Fprint(os.Stdout, lines)
			if !strings.HasSuffix(lines, "\n") {
				fmt.Fprintln(os.Stdout)
			}
		} else {
			ui.Detail("(no log output yet)")
		}
		offset.Store(off)
	}
	ui.Blank()

	ui.Title("How to use this")
	ui.Info("Type a command and press " + ui.Blue("Enter") + " to send it.")
	ui.Info("Press " + ui.Blue("Ctrl+C") + " to leave — the server " + ui.Bold("keeps running") + ".")
	ui.Detail("If the server crashes, this console exits on its own.")
	ui.Detail("Full shutdown: " + ui.Blue("./pastel stop"))
	ui.Blank()
	ui.Title("Handy commands")
	helpCmd := func(cmd, desc string) {
		fmt.Fprintf(ui.Out, "  %s  %s\n", ui.Blue(padAttachCmd(cmd)), desc)
	}
	helpCmd("list", "Who is online")
	helpCmd("say <message>", "Broadcast a chat message")
	helpCmd("op <player>", "Make someone an operator")
	helpCmd("deop <player>", "Remove operator status")
	helpCmd("kick <player>", "Disconnect a player")
	helpCmd("whitelist add <player>", "Allow someone when whitelist is on")
	helpCmd("gamemode survival <player>", "Set a player's mode")
	helpCmd("tp <player> <x> <y> <z>", "Teleport a player")
	helpCmd("time set day", "Change time (night, noon, …)")
	helpCmd("weather clear", "Change weather (rain, thunder)")
	helpCmd("save-all", "Force a world save")
	helpCmd("stop", "Shut down the server")
	ui.Blank()
	fmt.Fprintln(os.Stdout, ui.Dim("——— live (type below) ———"))

	done := make(chan struct{})
	defer close(done)
	go followLog(consoleLog, &offset, done)

	type lineEvent struct {
		text string
		err  error
		eof  bool
	}
	lines := make(chan lineEvent, 1)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		buf := make([]byte, 0, 64*1024)
		sc.Buffer(buf, 1024*1024)
		for sc.Scan() {
			lines <- lineEvent{text: sc.Text()}
		}
		if err := sc.Err(); err != nil && err != io.EOF {
			lines <- lineEvent{err: err, eof: true}
			return
		}
		lines <- lineEvent{eof: true}
	}()

	aliveTick := time.NewTicker(400 * time.Millisecond)
	defer aliveTick.Stop()

	leaveBecauseDead := func() {
		flushLog(consoleLog, &offset)
		ui.Blank()
		ui.Warn("The server is no longer running — left the console.")
		ui.Detail("If it crashed, see " + ui.Blue("logs/latest.log"))
		cleanupServerFiles(root)
	}

	for {
		select {
		case <-aliveTick.C:
			if _, still, err := Running(root); err != nil {
				return err
			} else if !still {
				leaveBecauseDead()
				return nil
			}
		case ev := <-lines:
			if ev.err != nil {
				return ev.err
			}
			if ev.eof {
				ui.Blank()
				if _, still, _ := Running(root); still {
					ui.OK("Left the console. Server is still running.")
					ui.Detail("Use ./pastel stop when you want to shut it down.")
				} else {
					ui.Warn("Left the console. Server is not running.")
				}
				return nil
			}
			line := strings.TrimSpace(ev.text)
			if line == "" {
				continue
			}
			fmt.Fprintf(os.Stdout, "\r%s\033[K\n", ui.Pink("» ")+line)
			if err := SendCommand(root, line); err != nil {
				ui.Warn(err.Error())
				if _, still, _ := Running(root); !still {
					leaveBecauseDead()
					return nil
				}
			}
		}
	}
}

func followLog(path string, offset *atomic.Int64, done <-chan struct{}) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			st, err := f.Stat()
			if err != nil {
				f.Close()
				continue
			}
			cur := offset.Load()
			size := st.Size()
			if size < cur {
				cur = 0
			}
			if size > cur {
				if _, err := f.Seek(cur, io.SeekStart); err == nil {
					n, _ := io.Copy(os.Stdout, f)
					offset.Store(cur + n)
				}
			}
			f.Close()
		}
	}
}

func flushLog(path string, offset *atomic.Int64) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return
	}
	cur := offset.Load()
	if st.Size() > cur {
		if _, err := f.Seek(cur, io.SeekStart); err == nil {
			n, _ := io.Copy(os.Stdout, f)
			offset.Store(cur + n)
		}
	}
}

func padAttachCmd(s string) string {
	const w = 28
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

func tailLines(path string, n int) (string, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	if len(data) == 0 {
		return "", 0, nil
	}
	i := len(data) - 1
	if data[i] == '\n' {
		i--
	}
	lines := 0
	start := 0
	for ; i >= 0; i-- {
		if data[i] == '\n' {
			lines++
			if lines >= n {
				start = i + 1
				break
			}
		}
	}
	return string(data[start:]), int64(len(data)), nil
}
