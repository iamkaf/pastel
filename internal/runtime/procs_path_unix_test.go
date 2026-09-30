//go:build !windows

package runtime

import "testing"

func TestCommandContainsPathStopsAtPathBoundaries(t *testing.T) {
	if commandContainsPath("java -jar /srv/mc-test/fabric-server.jar nogui", "/srv/mc") {
		t.Fatal("a sibling folder must not match")
	}
	if !commandContainsPath("java -jar /srv/mc/fabric-server.jar nogui", "/srv/mc") {
		t.Fatal("a path inside the root must match")
	}
}
