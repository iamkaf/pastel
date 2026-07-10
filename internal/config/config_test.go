package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.pastel")
	content := `
pack = "com.iamkaf.modpacks:forever-world:1.1.0"
memory = "4G"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pack != "com.iamkaf.modpacks:forever-world:1.1.0" {
		t.Fatal(cfg.Pack)
	}
	if cfg.Xmx() != "4G" {
		t.Fatal(cfg.Xmx())
	}
	if cfg.Root() != dir {
		t.Fatalf("root %s want %s", cfg.Root(), dir)
	}
	// No silent default Maven host.
	if repos := cfg.MavenRepositories(); len(repos) != 0 {
		t.Fatalf("expected empty repositories, got %v", repos)
	}
}

func TestSyncOnRunDefault(t *testing.T) {
	cfg := &Config{}
	if !cfg.ShouldSyncOnRun() {
		t.Fatal("default should sync on run")
	}
	f := false
	cfg.SyncOnRun = &f
	if cfg.ShouldSyncOnRun() {
		t.Fatal("explicit false")
	}
	tr := true
	cfg.SyncOnRun = &tr
	if !cfg.ShouldSyncOnRun() {
		t.Fatal("explicit true")
	}
}

func TestWriteSeedsSyncOnRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.pastel")
	if err := Write(WriteOptions{Path: path, Pack: "modrinth:example", Memory: "4G"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "sync_on_run = true") {
		t.Fatalf("expected seeded sync_on_run, got:\n%s", data)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ShouldSyncOnRun() {
		t.Fatal("loaded config should sync on run")
	}
}

func TestRepositoriesList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.pastel")
	content := `
pack = "com.iamkaf.modpacks:forever-world:1.1.0"
repositories = [
  "https://maven.example.com/",
  "https://maven.kaf.sh",
  "https://maven.example.com",
]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	repos := cfg.MavenRepositories()
	if len(repos) != 2 {
		t.Fatalf("want 2 unique repos, got %v", repos)
	}
	if repos[0] != "https://maven.example.com" || repos[1] != "https://maven.kaf.sh" {
		t.Fatalf("order/dedupe: %v", repos)
	}
}

func TestWriteEscapesTOMLStrings(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "server.pastel")
	want := `https://example.invalid/pack"name.mrpack`
	if err := Write(WriteOptions{Path: path, Pack: want, Memory: "4G"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pack != want {
		t.Fatalf("pack = %q, want %q", cfg.Pack, want)
	}
}
