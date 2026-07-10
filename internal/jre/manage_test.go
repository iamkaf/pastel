package jre

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeArchiveRel(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", ".", "../escape", "a/../../escape", "/absolute", `C:\escape`} {
		if _, err := safeArchiveRel(bad); err == nil {
			t.Fatalf("safeArchiveRel(%q) unexpectedly succeeded", bad)
		}
	}
	if got, err := safeArchiveRel("jdk/bin/java"); err != nil || got != filepath.Join("jdk", "bin", "java") {
		t.Fatalf("safeArchiveRel(valid) = %q, %v", got, err)
	}
}

func TestDownloadFileVerifiesChecksumAndSize(t *testing.T) {
	t.Parallel()
	body := []byte("verified runtime archive")
	sum := sha256.Sum256(body)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "runtime.tar.gz")
	if err := downloadFile(server.URL, dest, hex.EncodeToString(sum[:]), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != string(body) {
		t.Fatalf("downloaded body = %q, %v", got, err)
	}
}

func TestDownloadFileRejectsBadChecksum(t *testing.T) {
	t.Parallel()
	body := []byte("tampered")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "runtime.tar.gz")
	if err := downloadFile(server.URL, dest, strings.Repeat("0", sha256.Size*2), int64(len(body))); err == nil {
		t.Fatal("downloadFile unexpectedly accepted a bad checksum")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("destination should not exist after checksum failure: %v", err)
	}
}
