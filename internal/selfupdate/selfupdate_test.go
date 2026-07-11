package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRunVerifiesAndReplacesExecutable(t *testing.T) {
	archive := archiveName(runtime.GOOS, runtime.GOARCH)
	if archive == "" {
		t.Skip("unsupported test platform")
	}
	want := []byte("new pastel binary")
	archiveBody := testArchive(t, archive, want)
	hash := sha256.Sum256(archiveBody)

	mux := http.NewServeMux()
	mux.HandleFunc("/SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%x  %s\n", hash, archive)
	})
	mux.HandleFunc("/"+archive, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveBody)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	executable := filepath.Join(t.TempDir(), "pastel")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	if err := os.WriteFile(executable, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := Run(Options{Executable: executable, ReleaseBase: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Archive != archive {
		t.Fatalf("archive = %q", result.Archive)
	}
	got, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("installed %q, want %q", got, want)
	}
}

func TestRunRejectsChecksumMismatch(t *testing.T) {
	archive := archiveName(runtime.GOOS, runtime.GOARCH)
	if archive == "" {
		t.Skip("unsupported test platform")
	}
	archiveBody := testArchive(t, archive, []byte("untrusted"))
	mux := http.NewServeMux()
	mux.HandleFunc("/SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%064x  %s\n", 0, archive)
	})
	mux.HandleFunc("/"+archive, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveBody)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	executable := filepath.Join(t.TempDir(), "pastel")
	if err := os.WriteFile(executable, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{Executable: executable, ReleaseBase: server.URL, Client: server.Client()}); err == nil {
		t.Fatal("expected checksum failure")
	}
	got, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("executable changed to %q", got)
	}
}

func testArchive(t *testing.T, archive string, binary []byte) []byte {
	t.Helper()
	dir := stringsTrimArchiveSuffix(archive)
	if filepath.Ext(archive) == ".zip" {
		var body bytes.Buffer
		zw := zip.NewWriter(&body)
		file, err := zw.Create(dir + "/pastel.exe")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(binary); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return body.Bytes()
	}
	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	tw := tar.NewWriter(gz)
	name := dir + "/pastel"
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func stringsTrimArchiveSuffix(name string) string {
	if len(name) > len(".tar.gz") && name[len(name)-len(".tar.gz"):] == ".tar.gz" {
		return name[:len(name)-len(".tar.gz")]
	}
	return name[:len(name)-len(".zip")]
}
