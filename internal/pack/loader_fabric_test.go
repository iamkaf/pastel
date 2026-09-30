package pack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFabricJarMatches(t *testing.T) {
	name := "fabric-server-mc.26.2-loader.0.19.3-launcher.1.1.1.jar"
	if !fabricJarMatches(name, "26.2", "0.19.3") {
		t.Fatal("expected match")
	}
	if fabricJarMatches(name, "26.1.2", "0.19.3") {
		t.Fatal("mc mismatch")
	}
	if fabricJarMatches(name, "26.2", "0.19.2") {
		t.Fatal("loader mismatch")
	}
}

func TestEnsureFabricServerJarReplacesStale(t *testing.T) {
	root := t.TempDir()
	// Simulate leftover 26.1.2 launcher from forever-world 1.0.1
	stale := "fabric-server-mc.26.1.2-loader.0.19.2-launcher.1.1.1.jar"
	if err := os.WriteFile(filepath.Join(root, stale), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Matching jar already present
	good := "fabric-server-mc.26.2-loader.0.19.3-launcher.1.1.1.jar"
	if err := os.WriteFile(filepath.Join(root, good), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	name, changed, err := ensureFabricServerJar(root, "26.2", "0.19.3")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("should reuse matching jar without download")
	}
	if name != good {
		t.Fatalf("got %s", name)
	}
	if _, err := os.Stat(filepath.Join(root, stale)); !os.IsNotExist(err) {
		t.Fatal("stale launcher should be removed")
	}
	if _, err := os.Stat(filepath.Join(root, good)); err != nil {
		t.Fatal("good launcher should remain")
	}
}

func TestEnsureFabricLoaderUsesDeps(t *testing.T) {
	root := t.TempDir()
	stale := "fabric-server-mc.26.1.2-loader.0.19.2-launcher.1.1.1.jar"
	_ = os.WriteFile(filepath.Join(root, stale), []byte("old"), 0o644)

	m := &Manifest{
		Name:    "t",
		Version: "1",
		Dependencies: map[string]string{
			"minecraft":     "26.2",
			"fabric-loader": "0.19.3",
		},
	}
	// Without network: place the expected jar so ensure does not download
	good := "fabric-server-mc.26.2-loader.0.19.3-launcher.9.9.9.jar"
	_ = os.WriteFile(filepath.Join(root, good), []byte("new"), 0o644)

	changed, err := ensureFabricLoader(root, m)
	if err != nil {
		t.Fatal(err)
	}
	if m.Launch == nil || m.Launch.Jar != good {
		t.Fatalf("launch=%+v", m.Launch)
	}
	if changed {
		t.Fatal("reuse should not report changed when matching jar exists")
	}
	if _, err := os.Stat(filepath.Join(root, stale)); !os.IsNotExist(err) {
		t.Fatal("stale should be gone")
	}
}

func TestEnsureLoaderUsesTheExactNeoForgeVersion(t *testing.T) {
	root := t.TempDir()
	// 21.1.99 sorts after 21.1.200 as text; neither must stand in for the pack's version.
	for _, ver := range []string{"21.1.99", "21.1.200"} {
		dir := filepath.Join(root, "libraries", "net", "neoforged", "neoforge", ver)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, PreferredArgsFileName()), []byte("-p libraries\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := &Manifest{Dependencies: map[string]string{"minecraft": "1.21.1", "neoforge": "21.1.200"}}
	changed, err := EnsureLoader(root, m, "")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("an installed matching loader must be reused")
	}
	want := "libraries/net/neoforged/neoforge/21.1.200/" + PreferredArgsFileName()
	if m.Launch == nil || m.Launch.ArgsFile != want {
		t.Fatalf("launch = %+v, want args file %s", m.Launch, want)
	}
	if _, ok := argsFileLaunch(root, "neoforge", "21.1.300"); ok {
		t.Fatal("a different installed version must not satisfy a pack upgrade")
	}
}
