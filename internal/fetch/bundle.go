package fetch

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/iamkaf/pastel/internal/maven"
	"github.com/iamkaf/pastel/internal/pack"
)

// EnsureBundle downloads a zip/jar bundle and extracts it under root/path.
// Returns true if the archive was re-downloaded / re-extracted.
func (d *Downloader) EnsureBundle(b pack.Bundle, serverRoot string) (changed bool, err error) {
	algo, want, ok := b.PreferredHash()
	if !ok {
		return false, fmt.Errorf("bundle %s: no hash", b.ID)
	}

	cacheDir := filepath.Join(serverRoot, ".pastel", "bundles")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return false, err
	}
	cachePath := filepath.Join(cacheDir, b.ID+".zip")

	needDownload := true
	if st, err := os.Stat(cachePath); err == nil && !st.IsDir() {
		match, err := FileMatches(cachePath, algo, want)
		if err != nil {
			return false, err
		}
		if match {
			needDownload = false
		}
	}

	if needDownload {
		data, err := d.downloadBundleBytes(b)
		if err != nil {
			return false, err
		}
		got, err := HashBytes(data, algo)
		if err != nil {
			return false, err
		}
		if got != want {
			return false, fmt.Errorf("bundle %s: hash mismatch (want %s, got %s)", b.ID, want, got)
		}
		if err := os.WriteFile(cachePath, data, 0o644); err != nil {
			return false, err
		}
	}

	// Marker: skip extract if already applied this hash
	marker := filepath.Join(cacheDir, b.ID+".applied")
	if !needDownload {
		if prev, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(prev)) == want {
			return false, nil
		}
	}

	target := filepath.Join(serverRoot, filepath.FromSlash(b.Path))
	if err := extractZipFile(cachePath, target); err != nil {
		return false, fmt.Errorf("bundle %s extract: %w", b.ID, err)
	}
	if err := os.WriteFile(marker, []byte(want+"\n"), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func (d *Downloader) downloadBundleBytes(b pack.Bundle) ([]byte, error) {
	urls := append([]string{}, b.Downloads...)
	if b.Maven != "" {
		coord, err := maven.ParseCoordinate(b.Maven)
		if err != nil {
			return nil, err
		}
		for _, base := range d.Maven.Bases {
			urls = append(urls, coord.URL(base, b.Ext()))
		}
	}
	var last error
	for _, u := range urls {
		data, err := d.getBytes(u)
		if err != nil {
			last = err
			continue
		}
		return data, nil
	}
	if last == nil {
		last = fmt.Errorf("no sources")
	}
	return nil, fmt.Errorf("bundle %s: %w", b.ID, last)
}

func (d *Downloader) getBytes(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if d.UserAgent != "" {
		req.Header.Set("User-Agent", d.UserAgent)
	}
	res, err := d.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil, fmt.Errorf("GET %s: %s", rawURL, res.Status)
	}
	const maxBundleBytes = 512 << 20
	if res.ContentLength > maxBundleBytes {
		return nil, fmt.Errorf("GET %s: bundle is too large", rawURL)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBundleBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBundleBytes {
		return nil, fmt.Errorf("GET %s: bundle is too large", rawURL)
	}
	return data, nil
}

func extractZipFile(zipPath, destDir string) error {
	data, err := os.ReadFile(zipPath)
	if err != nil {
		return err
	}
	return ExtractZip(data, destDir)
}

// ExtractZip extracts a zip archive into destDir (creates it).
// Entries may be rooted at "config/" or be relative files; both work.
func ExtractZip(data []byte, destDir string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, zf := range zr.File {
		name, err := safeArchiveName(zf.Name)
		if err != nil {
			return err
		}
		// Strip a single leading config/ if present so bundle path=config is correct
		// when the zip was built with config/ as root prefix.
		// Actually authoring zips with paths relative to config/ (no prefix). Keep as-is.
		if zf.FileInfo().IsDir() {
			if err := root.MkdirAll(name, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, zf.Mode().Perm())
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func safeArchiveName(raw string) (string, error) {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), `\`, "/")
	if raw == "" || strings.HasPrefix(raw, "/") || strings.ContainsRune(raw, '\x00') {
		return "", fmt.Errorf("illegal path in zip: %s", raw)
	}
	parts := strings.Split(strings.TrimSuffix(raw, "/"), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("illegal path in zip: %s", raw)
		}
	}
	if len(parts[0]) >= 2 && parts[0][1] == ':' {
		return "", fmt.Errorf("illegal path in zip: %s", raw)
	}
	return filepath.FromSlash(strings.Join(parts, "/")), nil
}
