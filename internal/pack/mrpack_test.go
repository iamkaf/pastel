package pack

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMrpackIndexAndFilterEnv(t *testing.T) {
	raw := []byte(`{
  "formatVersion": 1,
  "game": "minecraft",
  "versionId": "1.0.0",
  "name": "Test Pack",
  "dependencies": {
    "minecraft": "26.2",
    "fabric-loader": "0.19.3"
  },
  "files": [
    {
      "path": "mods/server-mod.jar",
      "hashes": {"sha512": "aa", "sha1": "bb"},
      "downloads": ["https://cdn.modrinth.com/data/x/versions/y/server-mod.jar"],
      "fileSize": 1,
      "env": {"client": "unsupported", "server": "required"}
    },
    {
      "path": "mods/client-only.jar",
      "hashes": {"sha512": "cc"},
      "downloads": ["https://cdn.modrinth.com/data/x/versions/y/client-only.jar"],
      "env": {"client": "required", "server": "unsupported"}
    },
    {
      "path": "mods/both.jar",
      "hashes": {"sha512": "dd"},
      "downloads": ["https://cdn.modrinth.com/data/x/versions/y/both.jar"]
    }
  ]
}`)
	idx, err := ParseMrpackIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	loaded := &LoadedMrpack{Index: idx}
	m := loaded.ToManifest(SideServer)
	if m.Name != "Test Pack" || m.Version != "1.0.0" {
		t.Fatalf("meta: %+v", m)
	}
	if m.Minecraft() != "26.2" || m.LoaderKind() != "Fabric" {
		t.Fatalf("deps: mc=%s loader=%s", m.Minecraft(), m.LoaderKind())
	}
	if len(m.Files) != 2 {
		t.Fatalf("expected 2 server files, got %d", len(m.Files))
	}
	for _, f := range m.Files {
		if strings.Contains(f.Path, "client-only") {
			t.Fatalf("client-only should be filtered: %s", f.Path)
		}
	}
	if m.ModCount() != 2 {
		t.Fatalf("mod count %d", m.ModCount())
	}
}

func TestMrpackRejectsDotDot(t *testing.T) {
	raw := []byte(`{
  "formatVersion": 1,
  "versionId": "1",
  "name": "x",
  "files": [{
    "path": "mods/../evil.jar",
    "hashes": {"sha512": "aa"},
    "downloads": ["https://example.com/x"]
  }]
}`)
	if _, err := ParseMrpackIndex(raw); err == nil {
		t.Fatal("expected path error")
	}
}

func TestValidateMrpackPathIsPortable(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"/absolute", "../escape", "mods/../escape", `mods\escape.jar`, "mods//escape.jar", "C:/escape"} {
		if err := validateMrpackPath(bad); err == nil {
			t.Fatalf("validateMrpackPath(%q) unexpectedly succeeded", bad)
		}
	}
	if err := validateMrpackPath("mods/a..b.jar"); err != nil {
		t.Fatalf("valid filename with two dots rejected: %v", err)
	}
}

func TestIsMrpackIndexJSON(t *testing.T) {
	if !IsMrpackIndexJSON([]byte(`{"formatVersion":1,"versionId":"1","name":"n","files":[]}`)) {
		t.Fatal("expected mrpack")
	}
	if IsMrpackIndexJSON([]byte(`{"schemaVersion":1,"name":"n","version":"1","files":[]}`)) {
		t.Fatal("pastel schema should not count as mrpack")
	}
}

func TestMrpackZipOverrides(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "test.mrpack")
	if err := writeTestMrpack(zipPath); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadMrpack(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ZipPath == "" {
		t.Fatal("expected ZipPath")
	}
	m := loaded.ToManifest(SideServer)
	if len(m.Files) != 1 || m.Files[0].Path != "mods/demo.jar" {
		t.Fatalf("files: %+v", m.Files)
	}

	root := filepath.Join(dir, "server")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	written, err := loaded.ApplyOverrides(root, SideServer)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) < 2 {
		t.Fatalf("expected overrides + server-overrides, got %d files: %v", len(written), written)
	}
	// Override-shipped mod jars must be allowlisted for prune
	jars := OverrideModJars(written)
	if len(jars) != 1 || jars[0] != "from-override.jar" {
		t.Fatalf("OverrideModJars from apply: %v (written=%v)", jars, written)
	}
	listed, err := loaded.ListOverrideModJars(SideServer)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0] != "from-override.jar" {
		t.Fatalf("ListOverrideModJars: %v", listed)
	}
	if _, err := os.Stat(filepath.Join(root, "mods", "from-override.jar")); err != nil {
		t.Fatal("override mod jar missing on disk")
	}
	// server-overrides should win for shared path
	body, err := os.ReadFile(filepath.Join(root, "config", "demo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "server=1\n" {
		t.Fatalf("server-overrides should win, got %q", body)
	}
	if _, err := os.Stat(filepath.Join(root, "eula.txt")); err != nil {
		t.Fatal("overrides eula missing")
	}
}

func TestResolveLocalMrpack(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pack.mrpack")
	if err := writeTestMrpack(zipPath); err != nil {
		t.Fatal(err)
	}
	res, err := Resolve(ResolveSpec{Raw: zipPath, Side: SideServer})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != "mrpack" || res.Mrpack == nil {
		t.Fatalf("format=%s mrpack=%v", res.Format, res.Mrpack != nil)
	}
	if res.Manifest.Name != "Zip Pack" {
		t.Fatalf("name %s", res.Manifest.Name)
	}
}

func TestMrpackZipDirectoryEntries(t *testing.T) {
	// Real .mrpack zips often include explicit directory records with a trailing
	// slash (e.g. server-overrides/config/bonded/). These must not fail install.
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "dirs.mrpack")
	if err := writeTestMrpackWithDirEntries(zipPath); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadMrpack(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "server")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	written, err := loaded.ApplyOverrides(root, SideServer)
	if err != nil {
		t.Fatalf("ApplyOverrides with zip directory entries: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "config", "bonded", "bonded-common.toml")); err != nil {
		t.Fatalf("bonded config missing: %v (written=%v)", err, written)
	}
	// Empty override directories should still be created.
	if st, err := os.Stat(filepath.Join(root, "config", "empty-only")); err != nil || !st.IsDir() {
		t.Fatalf("empty override dir missing: err=%v", err)
	}
}

func writeTestMrpack(path string) error {
	return writeTestMrpackEntries(path, map[string]string{
		"overrides/eula.txt":                "eula=true\n",
		"overrides/config/demo.toml":        "client=1\n",
		"overrides/mods/from-override.jar":  "fake-jar",
		"server-overrides/config/demo.toml": "server=1\n",
	}, nil)
}

func writeTestMrpackWithDirEntries(path string) error {
	return writeTestMrpackEntries(path, map[string]string{
		"server-overrides/config/bonded/bonded-common.toml": "x=1\n",
	}, []string{
		"server-overrides/config/",
		"server-overrides/config/bonded/",
		"server-overrides/config/empty-only/",
	})
}

func writeTestMrpackEntries(path string, files map[string]string, dirs []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	index := `{
  "formatVersion": 1,
  "game": "minecraft",
  "versionId": "9.9.9",
  "name": "Zip Pack",
  "dependencies": {"minecraft": "26.2", "fabric-loader": "0.19.3"},
  "files": [{
    "path": "mods/demo.jar",
    "hashes": {"sha512": "00", "sha1": "11"},
    "downloads": ["https://cdn.modrinth.com/data/x/versions/y/demo.jar"],
    "fileSize": 1,
    "env": {"client": "required", "server": "required"}
  }]
}`
	zw, err := w.Create("modrinth.index.json")
	if err != nil {
		return err
	}
	if _, err := zw.Write([]byte(index)); err != nil {
		return err
	}
	// Directory entries first, matching common zip tool ordering.
	for _, name := range dirs {
		if !strings.HasSuffix(name, "/") {
			name += "/"
		}
		if _, err := w.Create(name); err != nil {
			return err
		}
	}
	for name, body := range files {
		zw, err := w.Create(name)
		if err != nil {
			return err
		}
		if _, err := zw.Write([]byte(body)); err != nil {
			return err
		}
	}
	return w.Close()
}

