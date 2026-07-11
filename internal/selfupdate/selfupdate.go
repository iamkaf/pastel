// Package selfupdate securely replaces the running Pastel executable.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultReleaseBase = "https://github.com/iamkaf/pastel/releases/latest/download"
	maxDownloadBytes   = 128 << 20
	maxBinaryBytes     = 64 << 20
)

// Options controls a self-update. ReleaseBase is overrideable for tests.
type Options struct {
	Executable  string
	ReleaseBase string
	Client      *http.Client
}

// Result describes the installed release.
type Result struct {
	Archive string
	Tag     string
}

// Run downloads, verifies, extracts, and atomically installs the latest binary.
func Run(opt Options) (*Result, error) {
	executable := opt.Executable
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("find current executable: %w", err)
		}
	}
	executable, err := filepath.Abs(executable)
	if err != nil {
		return nil, fmt.Errorf("resolve current executable: %w", err)
	}
	base := strings.TrimRight(opt.ReleaseBase, "/")
	if base == "" {
		base = defaultReleaseBase
	}
	client := opt.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}

	archive := archiveName(runtime.GOOS, runtime.GOARCH)
	if archive == "" {
		return nil, fmt.Errorf("self-update is not available for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	sums, finalURL, err := download(client, base+"/SHA256SUMS", 1<<20)
	if err != nil {
		return nil, fmt.Errorf("download release checksums: %w", err)
	}
	expected, err := checksumFor(sums, archive)
	if err != nil {
		return nil, err
	}
	body, _, err := download(client, base+"/"+archive, maxDownloadBytes)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", archive, err)
	}
	actual := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(actual[:]), expected) {
		return nil, fmt.Errorf("checksum verification failed for %s", archive)
	}
	binary, err := extractBinary(archive, body)
	if err != nil {
		return nil, err
	}
	if err := replaceExecutable(executable, binary); err != nil {
		return nil, fmt.Errorf("replace %s: %w", executable, err)
	}
	return &Result{Archive: archive, Tag: releaseTag(finalURL)}, nil
}

func archiveName(goos, goarch string) string {
	if goarch != "amd64" && goarch != "arm64" {
		return ""
	}
	switch goos {
	case "linux", "darwin":
		return fmt.Sprintf("pastel-%s-%s.tar.gz", goos, goarch)
	case "windows":
		return fmt.Sprintf("pastel-%s-%s.zip", goos, goarch)
	default:
		return ""
	}
}

func download(client *http.Client, url string, limit int64) ([]byte, string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("%s returned %s", url, resp.Status)
	}
	reader := io.LimitReader(resp.Body, limit+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > limit {
		return nil, "", fmt.Errorf("download exceeded %d bytes", limit)
	}
	return body, resp.Request.URL.String(), nil
}

func checksumFor(sums []byte, archive string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(fields[1], "*"), "./")
		if name == archive {
			if len(fields[0]) != sha256.Size*2 {
				break
			}
			if _, err := hex.DecodeString(fields[0]); err != nil {
				break
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("release checksum is missing or invalid for %s", archive)
}

func extractBinary(archive string, body []byte) ([]byte, error) {
	want := strings.TrimSuffix(strings.TrimSuffix(archive, ".tar.gz"), ".zip") + "/pastel"
	if strings.HasSuffix(archive, ".zip") {
		want += ".exe"
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return nil, fmt.Errorf("open release archive: %w", err)
		}
		for _, file := range zr.File {
			if filepath.ToSlash(file.Name) != want {
				continue
			}
			if file.UncompressedSize64 > maxBinaryBytes {
				return nil, fmt.Errorf("release binary is unexpectedly large")
			}
			r, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return readBinary(r)
		}
		return nil, fmt.Errorf("release archive did not contain %s", want)
	}
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("open release archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release archive: %w", err)
		}
		if filepath.ToSlash(header.Name) == want && header.Typeflag == tar.TypeReg {
			if header.Size > maxBinaryBytes {
				return nil, fmt.Errorf("release binary is unexpectedly large")
			}
			return readBinary(tr)
		}
	}
	return nil, fmt.Errorf("release archive did not contain %s", want)
}

func readBinary(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxBinaryBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || len(body) > maxBinaryBytes {
		return nil, fmt.Errorf("release binary has an invalid size")
	}
	return body, nil
}

func releaseTag(url string) string {
	const marker = "/releases/download/"
	i := strings.Index(url, marker)
	if i < 0 {
		return ""
	}
	rest := url[i+len(marker):]
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		return rest[:slash]
	}
	return ""
}
