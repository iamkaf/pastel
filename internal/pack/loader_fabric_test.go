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
		SchemaVersion: SchemaVersion,
		Name:          "t",
		Version:       "1",
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
