package runtime

import (
	"os"
	"strconv"
	"testing"

	"github.com/iamkaf/pastel/internal/state"
)

func TestCleanupAfterAttachDeathPreservesLiveSupervisor(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(state.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.SupervisorPIDPath(root), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
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
