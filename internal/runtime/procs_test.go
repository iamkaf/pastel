package runtime

import (
	"os"
	"strconv"
	"testing"

	"github.com/iamkaf/pastel/internal/state"
)

func TestCwdMatchesRoot(t *testing.T) {
	if !cwdMatchesRoot("/srv/pastel", "/srv/pastel") {
		t.Fatal("exact")
	}
	if !cwdMatchesRoot("/srv/pastel (deleted)", "/srv/pastel") {
		t.Fatal("deleted suffix")
	}
	if cwdMatchesRoot("/srv/other", "/srv/pastel") {
		t.Fatal("mismatch")
	}
}

func TestLooksLikeMinecraftServerCmd(t *testing.T) {
	ok := []string{
		"java -Xmx4G -jar fabric-server-mc.26.2-loader.0.19.3-launcher.1.1.1.jar nogui",
		"/path/.pastel/jre/21/bin/java -Xmx4G -jar fabric-server-mc.1.21.1-loader.0.16.14-launcher.1.1.1.jar nogui",
		"java @user_jvm_args.txt @libraries/net/neoforged/neoforge/21/unix_args.txt nogui",
	}
	for _, c := range ok {
		if !looksLikeMinecraftServerCmd(c) {
			t.Fatalf("should match: %s", c)
		}
	}
	bad := []string{
		"java -jar gradle-server.jar",
		"java __hold-fifo /tmp/x",
		"/usr/bin/python3",
	}
	for _, c := range bad {
		if looksLikeMinecraftServerCmd(c) {
			t.Fatalf("should not match: %s", c)
		}
	}
}

func TestStalePIDFileDoesNotClaimAnUnrelatedProcess(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(state.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	// This test process is alive but is not a Pastel supervisor or a Minecraft server.
	self := []byte(strconv.Itoa(os.Getpid()) + "\n")
	for _, path := range []string{state.PIDPath(root), state.SupervisorPIDPath(root), state.HoldPIDPath(root)} {
		if err := os.WriteFile(path, self, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := serverPID(root); ok {
		t.Fatal("server pid file trusted an unrelated process")
	}
	if _, ok := supervisorPID(root); ok {
		t.Fatal("supervisor pid file trusted an unrelated process")
	}
	if _, ok := holdPID(root); ok {
		t.Fatal("hold pid file trusted an unrelated process")
	}
}
