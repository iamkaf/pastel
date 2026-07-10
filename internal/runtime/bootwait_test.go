package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerDoneRE(t *testing.T) {
	samples := []string{
		`[04:19:30] [Server thread/INFO]: Done (0.652s)! For help, type "help"`,
		`Done (1.0s)! For help, type "help"`,
	}
	for _, s := range samples {
		if !serverDoneRE.MatchString(s) {
			t.Fatalf("should match: %q", s)
		}
	}
}

func TestServerFailedRE(t *testing.T) {
	samples := []string{
		`[04:45:16] [main/ERROR]: Failed to start the minecraft server`,
		`java.lang.RuntimeException: Could not execute entrypoint stage 'main'`,
	}
	for _, s := range samples {
		if !serverFailedRE.MatchString(s) {
			t.Fatalf("should match failure: %q", s)
		}
	}
	if serverFailedRE.MatchString(`Preparing spawn area: 80%`) {
		t.Fatal("false positive")
	}
}

func TestLogContainsDoneSinceIgnoresOldDone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "console.log")
	old := `[old] Done (0.1s)! For help, type "help"` + "\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	startOff := logSize(path)
	if logContainsDoneSince(path, startOff) {
		t.Fatal("must not treat previous run's Done as this boot")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`[04:21:28] [Server thread/INFO]: Done (0.654s)! For help, type "help"` + "\n")
	_, _ = f.WriteString(`[04:21:28] ThreadedAnvilChunkStorage: All dimensions are saved` + "\n")
	f.Close()
	if !logContainsDoneSince(path, startOff) {
		t.Fatal("new Done should be detected")
	}
	last := lastNonEmptyLogLineSince(path, startOff)
	if !strings.Contains(last, "dimensions are saved") {
		t.Fatalf("last new line: %q", last)
	}
}

func TestLogContainsFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "latest.log")
	body := `Loading mods...
[main/ERROR]: Failed to start the minecraft server
java.lang.RuntimeException: Could not execute entrypoint
... 7 more
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if !logContainsFailureSince(path, 0) {
		t.Fatal("expected failure detection")
	}
}

func TestTruncateRunes(t *testing.T) {
	got := truncateRunes("abcdefghijklmnopqrstuvwxyz", 10)
	if got != "abcdefghi…" {
		t.Fatalf("%q", got)
	}
}
