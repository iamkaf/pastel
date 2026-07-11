package runtime

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSummarizeClientOnlyCrash(t *testing.T) {
	log := `
[main/ERROR]: Failed to start the minecraft server
java.lang.RuntimeException: Could not execute entrypoint stage 'main' due to errors, provided by 'amberdreams' at 'com.iamkaf.amberdreams.fabric.AmberDreamsFabric'!
Caused by: java.lang.NoClassDefFoundError: net/fabricmc/fabric/api/client/rendering/v1/WorldRenderEvents
~[amberdreams-fabric-1.21.1-0.2.0-alpha.1.jar:?]
~[dynamicedge-fabric-1.21.1-1.0.0-alpha.2.jar:?]
`
	s := summarizeCrash(log)
	if s.Headline == "" || !strings.Contains(strings.ToLower(s.Headline), "client") {
		t.Fatalf("headline: %q", s.Headline)
	}
	if len(s.Mods) == 0 {
		t.Fatal("expected mod names")
	}
	found := false
	for _, m := range s.Mods {
		if strings.Contains(strings.ToLower(m), "amberdreams") {
			found = true
		}
	}
	if !found {
		t.Fatalf("mods: %v", s.Mods)
	}
}

func TestShouldRestartServer(t *testing.T) {
	crashed := exec.Command("sh", "-c", "exit 7")
	if err := crashed.Run(); err == nil {
		t.Fatal("expected crash command to fail")
	} else {
		if !shouldRestartServer(err, true, true) {
			t.Fatal("ready server should restart after non-zero exit")
		}
		if shouldRestartServer(err, false, true) {
			t.Fatal("startup failure must not restart")
		}
		if shouldRestartServer(err, true, false) {
			t.Fatal("disabled auto restart must not restart")
		}
	}
	if shouldRestartServer(nil, true, true) {
		t.Fatal("clean shutdown must not restart")
	}
}
