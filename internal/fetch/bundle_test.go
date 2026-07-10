package fetch

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractZipRejectsTraversal(t *testing.T) {
	t.Parallel()
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	w, err := zw.Create("../escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("escape"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	parent := t.TempDir()
	dest := filepath.Join(parent, "dest")
	if err := ExtractZip(archive.Bytes(), dest); err == nil {
		t.Fatal("ExtractZip unexpectedly accepted traversal")
	}
	if _, err := os.Stat(filepath.Join(parent, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("traversal target should not exist: %v", err)
	}
}

func TestExtractZipCannotFollowEscapingDestinationSymlink(t *testing.T) {
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	w, err := zw.Create("config/server.toml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("safe = true"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	parent := t.TempDir()
	dest := filepath.Join(parent, "dest")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dest, "config")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := ExtractZip(archive.Bytes(), dest); err == nil {
		t.Fatal("ExtractZip unexpectedly followed an escaping symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "server.toml")); !os.IsNotExist(err) {
		t.Fatalf("outside target should not exist: %v", err)
	}
}
