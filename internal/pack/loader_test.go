package pack

import "testing"

func TestInstallerArtifactVersion(t *testing.T) {
	if got := installerArtifactVersion("forge", "1.20.1", "47.2.0"); got != "1.20.1-47.2.0" {
		t.Fatalf("forge short: %s", got)
	}
	if got := installerArtifactVersion("forge", "1.20.1", "1.20.1-47.2.0"); got != "1.20.1-47.2.0" {
		t.Fatalf("forge full: %s", got)
	}
	if got := installerArtifactVersion("neoforge", "26.1.2", "21.1.0"); got != "21.1.0" {
		t.Fatalf("neoforge: %s", got)
	}
}

func TestInstallerDownloadURL(t *testing.T) {
	u := installerDownloadURL("neoforge", "21.1.172")
	want := "https://maven.neoforged.net/releases/net/neoforged/neoforge/21.1.172/neoforge-21.1.172-installer.jar"
	if u != want {
		t.Fatalf("got %s", u)
	}
	u = installerDownloadURL("forge", "1.20.1-47.2.0")
	want = "https://maven.minecraftforge.net/net/minecraftforge/forge/1.20.1-47.2.0/forge-1.20.1-47.2.0-installer.jar"
	if u != want {
		t.Fatalf("got %s", u)
	}
}
