//go:build windows

package runtime

import (
	"os"
	"testing"
	"time"

	"github.com/iamkaf/pastel/internal/state"
)

func TestWindowsConsoleSendCommand(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(state.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinR.Close()

	ln, pipeName, err := listenConsolePipe()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := os.WriteFile(state.ConsoleInPath(root), []byte("pipe:"+pipeName+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	feeder := &consoleFeeder{}
	feeder.setWriter(stdinW)
	go feeder.acceptLoop(ln)

	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := stdinR.Read(buf)
		done <- string(buf[:n])
	}()

	if err := SendCommand(root, "say hello"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got != "say hello\n" && got != "say hello\r\n" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for console command")
	}
}

func TestParseWMICJava(t *testing.T) {
	raw := "CommandLine=java -Xmx4G -jar C:\\srv\\fabric-server-mc.jar nogui\r\nProcessId=4242\r\n\r\n"
	infos := parseWMICJava(`C:\srv`, raw)
	if len(infos) != 1 || infos[0].PID != 4242 {
		t.Fatalf("%+v", infos)
	}
}
