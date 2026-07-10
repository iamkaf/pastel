// Package fetch downloads files and verifies content hashes.
package fetch

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iamkaf/pastel/internal/maven"
	"github.com/iamkaf/pastel/internal/pack"
)

// Downloader fetches remote bytes with hash verification.
type Downloader struct {
	HTTP      *http.Client
	Maven     *maven.Client
	UserAgent string
}

// New returns a Downloader with defaults.
// mavenBases is an ordered repository list (empty → Kaf Maven).
func New(mavenBases ...string) *Downloader {
	return &Downloader{
		HTTP:      &http.Client{Timeout: 10 * time.Minute},
		Maven:     maven.NewClient(mavenBases...),
		UserAgent: "Pastel/0.1 (+https://kaf.sh)",
	}
}

// EnsureFile makes sure dest matches the pack file entry (download if needed).
// Returns true if a download or rewrite occurred.
func (d *Downloader) EnsureFile(f pack.File, dest string) (changed bool, err error) {
	algo, want, ok := f.PreferredHash()
	if !ok {
		return false, fmt.Errorf("%s: no hash", f.Path)
	}
	if st, err := os.Stat(dest); err == nil && !st.IsDir() {
		match, err := FileMatches(dest, algo, want)
		if err != nil {
			return false, err
		}
		if match {
			return false, nil
		}
	}

	urls := append([]string{}, f.Downloads...)
	if f.Maven != "" {
		coord, err := maven.ParseCoordinate(f.Maven)
		if err != nil {
			return false, fmt.Errorf("%s: %w", f.Path, err)
		}
		ext := "jar"
		if strings.HasSuffix(strings.ToLower(f.Path), ".json") {
			ext = "json"
		} else if i := strings.LastIndex(f.Path, "."); i >= 0 {
			ext = f.Path[i+1:]
		}
		for _, base := range d.Maven.Bases {
			urls = append(urls, coord.URL(base, ext))
		}
	}
	if len(urls) == 0 {
		return false, fmt.Errorf("%s: no download sources", f.Path)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return false, err
	}
	tmp := dest + ".pastel-tmp"
	defer os.Remove(tmp)

	var lastErr error
	for _, u := range urls {
		if err := d.downloadTo(u, tmp, f.FileSize); err != nil {
			lastErr = err
			continue
		}
		match, err := FileMatches(tmp, algo, want)
		if err != nil {
			lastErr = err
			continue
		}
		if !match {
			got, _ := HashFile(tmp, algo)
			lastErr = fmt.Errorf("%s: hash mismatch from %s (want %s %s, got %s)", f.Path, u, algo, want, got)
			continue
		}
		if err := replaceFile(tmp, dest); err != nil {
			return false, err
		}
		return true, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all sources failed")
	}
	return false, fmt.Errorf("%s: %w", f.Path, lastErr)
}

func (d *Downloader) downloadTo(rawURL, dest string, expectedSize int64) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	if d.UserAgent != "" {
		req.Header.Set("User-Agent", d.UserAgent)
	}
	res, err := d.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, res.Body)
		return fmt.Errorf("GET %s: %s", rawURL, res.Status)
	}
	const maxManagedFileBytes = int64(2 << 30)
	limit := maxManagedFileBytes
	if expectedSize > 0 {
		limit = expectedSize
		if res.ContentLength >= 0 && res.ContentLength != expectedSize {
			return fmt.Errorf("GET %s: size mismatch (want %d bytes, got %d)", rawURL, expectedSize, res.ContentLength)
		}
	} else if res.ContentLength > maxManagedFileBytes {
		return fmt.Errorf("GET %s: file is too large", rawURL)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	written, err := io.Copy(f, io.LimitReader(res.Body, limit+1))
	if err != nil {
		return err
	}
	if written > limit {
		return fmt.Errorf("GET %s: file is too large", rawURL)
	}
	if expectedSize > 0 && written != expectedSize {
		return fmt.Errorf("GET %s: size mismatch (want %d bytes, got %d)", rawURL, expectedSize, written)
	}
	return nil
}

// FileMatches reports whether path's content hash equals wantHex.
func FileMatches(path, algo, wantHex string) (bool, error) {
	got, err := HashFile(path, algo)
	if err != nil {
		return false, err
	}
	return got == strings.ToLower(wantHex), nil
}

// HashFile returns the hex digest of a file.
func HashFile(path, algo string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h, err := newHash(algo)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashBytes returns the hex digest of b.
func HashBytes(b []byte, algo string) (string, error) {
	h, err := newHash(algo)
	if err != nil {
		return "", err
	}
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func newHash(algo string) (hash.Hash, error) {
	switch strings.ToLower(algo) {
	case "sha512":
		return sha512.New(), nil
	case "sha256":
		return sha256.New(), nil
	case "sha1":
		return sha1.New(), nil
	default:
		return nil, fmt.Errorf("unsupported hash algorithm %q", algo)
	}
}
