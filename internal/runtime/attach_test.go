package runtime

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/iamkaf/pastel/internal/state"
)

func TestCleanupAfterAttachDeathPreservesLiveSupervisor(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(state.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	supervisor := startFakeHelper(t, "__supervise", root)
	if err := os.WriteFile(state.SupervisorPIDPath(root), []byte(strconv.Itoa(supervisor)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.ConsoleInPath(root), []byte("endpoint\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cleanupAfterAttachDeath(root)

	if _, err := os.Stat(state.SupervisorPIDPath(root)); err != nil {
		t.Fatalf("live supervisor tracking was removed: %v", err)
	}
	if _, err := os.Stat(state.ConsoleInPath(root)); err != nil {
		t.Fatalf("live console endpoint was removed: %v", err)
	}
}

func TestCleanupAfterAttachDeathRemovesStaleFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(state.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.ConsoleInPath(root), []byte("endpoint\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cleanupAfterAttachDeath(root)

	if _, err := os.Stat(state.ConsoleInPath(root)); !os.IsNotExist(err) {
		t.Fatalf("stale console endpoint remains: %v", err)
	}
}

// startFakeHelper runs this test binary as a long-lived process whose command line
// looks like a Pastel helper for root, then returns its PID.
func startFakeHelper(t *testing.T, subcommand, root string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFakeHelperProcess$", "--", subcommand, root)
	cmd.Env = append(os.Environ(), "PASTEL_FAKE_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

func TestFakeHelperProcess(t *testing.T) {
	if os.Getenv("PASTEL_FAKE_HELPER") != "1" {
		t.Skip("helper process for other tests")
	}
	time.Sleep(time.Minute)
}
