package jre

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/iamkaf/pastel/internal/ui"
)

// Dir returns the managed JRE root for a server: <server>/.pastel/jre
func Dir(serverRoot string) string {
	return filepath.Join(serverRoot, ".pastel", "jre")
}

// Ensure returns a java executable that satisfies requiredMajor.
// Prefer explicit override if it meets the requirement; otherwise use/download a managed JRE.
func Ensure(serverRoot string, requiredMajor int, override string) (javaBin string, err error) {
	if requiredMajor <= 0 {
		requiredMajor = 25
	}

	// Explicit override path (from server.pastel): use if good enough.
	if override != "" && override != "java" {
		if ok, got, err := Meets(override, requiredMajor); err == nil && ok {
			ui.Detail(fmt.Sprintf("using configured Java %d (%s)", got, override))
			return override, nil
		} else if err == nil {
			ui.Warn(fmt.Sprintf("configured java is too old (have %d, need %d) — using managed JRE", got, requiredMajor))
		} else {
			ui.Warn(fmt.Sprintf("configured java unusable (%v) — using managed JRE", err))
		}
	} else {
		// PATH java is fine if new enough
		if ok, got, err := Meets("java", requiredMajor); err == nil && ok {
			ui.Detail(fmt.Sprintf("using system Java %d", got))
			return "java", nil
		}
	}

	bin, err := ensureManaged(serverRoot, requiredMajor)
	if err != nil {
		return "", err
	}
	ok, got, err := Meets(bin, requiredMajor)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("managed Java reports version %d, need %d", got, requiredMajor)
	}
	ui.Detail(fmt.Sprintf("using managed Java %d", got))
	return bin, nil
}

func ensureManaged(serverRoot string, major int) (string, error) {
	base := filepath.Join(Dir(serverRoot), fmt.Sprintf("%d", major))
	if bin, err := findJavaBinary(base); err == nil {
		return bin, nil
	}

	ui.Step(fmt.Sprintf("Downloading Java %d for this Minecraft version…", major))
	ui.Detail("Pastel keeps a private JRE under .pastel/jre/ (like a launcher)")
	if err := os.MkdirAll(Dir(serverRoot), 0o755); err != nil {
		return "", err
	}

	osName, arch, ext, err := platform()
	if err != nil {
		return "", err
	}
	pkg, err := resolvePackage(major, osName, arch)
	if err != nil {
		return "", fmt.Errorf("resolve Java %d: %w", major, err)
	}
	staging := base + ".install-tmp"
	if err := os.RemoveAll(staging); err != nil {
		return "", err
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)

	archivePath := filepath.Join(staging, "download"+ext)
	if err := downloadFile(pkg.Link, archivePath, pkg.Checksum, pkg.Size); err != nil {
		return "", fmt.Errorf("download Java %d: %w", major, err)
	}

	ui.Step("Installing Java…")
	if err := extractArchive(archivePath, staging); err != nil {
		return "", fmt.Errorf("extract Java %d: %w", major, err)
	}
	if _, err := findJavaBinary(staging); err != nil {
		return "", fmt.Errorf("Java %d installed but bin/java not found: %w", major, err)
	}
	if err := os.Remove(archivePath); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.RemoveAll(base); err != nil {
		return "", err
	}
	if err := os.Rename(staging, base); err != nil {
		return "", err
	}
	bin, err := findJavaBinary(base)
	if err != nil {
		return "", err
	}
	ui.OK(fmt.Sprintf("Java %d ready", major))
	return bin, nil
}

type packageAsset struct {
	Link     string `json:"link"`
	Checksum string `json:"checksum"`
	Size     int64  `json:"size"`
}

func resolvePackage(major int, osName, arch string) (packageAsset, error) {
	endpoint := fmt.Sprintf("https://api.adoptium.net/v3/assets/feature_releases/%d/ga", major)
	query := url.Values{
		"architecture": {arch},
		"heap_size":    {"normal"},
		"image_type":   {"jre"},
		"jvm_impl":     {"hotspot"},
		"os":           {osName},
		"page":         {"0"},
		"page_size":    {"1"},
		"project":      {"jdk"},
		"sort_order":   {"DESC"},
		"vendor":       {"eclipse"},
	}
	req, err := http.NewRequest(http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return packageAsset{}, err
	}
	req.Header.Set("User-Agent", "Pastel/0.1 (+https://kaf.sh)")
	client := &http.Client{Timeout: 60 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return packageAsset{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return packageAsset{}, fmt.Errorf("Temurin metadata: %s", res.Status)
	}
	var releases []struct {
		Binaries []struct {
			Package packageAsset `json:"package"`
		} `json:"binaries"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&releases); err != nil {
		return packageAsset{}, err
	}
	if len(releases) == 0 || len(releases[0].Binaries) == 0 {
		return packageAsset{}, fmt.Errorf("no matching Temurin JRE found")
	}
	pkg := releases[0].Binaries[0].Package
	if pkg.Link == "" || pkg.Checksum == "" || pkg.Size <= 0 {
		return packageAsset{}, fmt.Errorf("Temurin package metadata is incomplete")
	}
	if _, err := hex.DecodeString(pkg.Checksum); err != nil || len(pkg.Checksum) != sha256.Size*2 {
		return packageAsset{}, fmt.Errorf("Temurin package checksum is invalid")
	}
	return pkg, nil
}

func platform() (osName, arch, ext string, err error) {
	switch runtime.GOOS {
	case "linux":
		osName = "linux"
		ext = ".tar.gz"
	case "darwin":
		osName = "mac"
		ext = ".tar.gz"
	case "windows":
		osName = "windows"
		ext = ".zip"
	default:
		return "", "", "", fmt.Errorf("unsupported OS %s for managed Java", runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "aarch64"
	default:
		return "", "", "", fmt.Errorf("unsupported CPU %s for managed Java", runtime.GOARCH)
	}
	return osName, arch, ext, nil
}

func downloadFile(rawURL, dest, wantChecksum string, wantSize int64) error {
	client := &http.Client{
		Timeout: 15 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Pastel/0.1 (+https://kaf.sh)")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", rawURL, res.Status)
	}
	tmp := dest + ".tmp"
	defer os.Remove(tmp)
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	var written int64
	buf := make([]byte, 32*1024)
	var lastPrint time.Time
	total := res.ContentLength
	for {
		n, readErr := res.Body.Read(buf)
		if n > 0 {
			if _, err := io.MultiWriter(f, h).Write(buf[:n]); err != nil {
				return err
			}
			written += int64(n)
			if time.Since(lastPrint) > 500*time.Millisecond {
				if total > 0 {
					pct := float64(written) * 100 / float64(total)
					ui.Detail(fmt.Sprintf("download %.0f%% (%s / %s)", pct, humanBytes(written), humanBytes(total)))
				} else {
					ui.Detail(fmt.Sprintf("download %s…", humanBytes(written)))
				}
				lastPrint = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if wantSize > 0 && written != wantSize {
		return fmt.Errorf("size mismatch (want %d bytes, got %d)", wantSize, written)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, wantChecksum) {
		return fmt.Errorf("SHA-256 mismatch (want %s, got %s)", wantChecksum, got)
	}
	return os.Rename(tmp, dest)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func extractArchive(archivePath, destDir string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return unzip(archivePath, destDir)
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return untarGz(archivePath, destDir)
	default:
		return fmt.Errorf("unknown archive type: %s", archivePath)
	}
}

func untarGz(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		rel, err := safeArchiveRel(hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(rel, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
				return err
			}
			mode := hdr.FileInfo().Mode().Perm()
			out, err := root.OpenFile(rel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			link := filepath.FromSlash(strings.ReplaceAll(hdr.Linkname, `\`, "/"))
			if filepath.IsAbs(link) || filepath.VolumeName(link) != "" {
				return fmt.Errorf("illegal symlink in archive: %s -> %s", hdr.Name, hdr.Linkname)
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(rel), link))
			if resolved == ".." || strings.HasPrefix(resolved, ".."+string(os.PathSeparator)) {
				return fmt.Errorf("illegal symlink in archive: %s -> %s", hdr.Name, hdr.Linkname)
			}
			if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
				return err
			}
			_ = root.Remove(rel)
			if err := root.Symlink(link, rel); err != nil {
				return err
			}
		}
	}
	return nil
}

func unzip(path, dest string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, zf := range r.File {
		rel, err := safeArchiveRel(zf.Name)
		if err != nil {
			return err
		}
		if zf.FileInfo().IsDir() {
			if err := root.MkdirAll(rel, 0o755); err != nil {
				return err
			}
			continue
		}
		if zf.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not supported in zip archive: %s", zf.Name)
		}
		if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := root.OpenFile(rel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, zf.Mode().Perm())
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

func safeArchiveRel(name string) (string, error) {
	raw := strings.ReplaceAll(strings.TrimSpace(name), `\`, "/")
	if raw == "" || strings.HasPrefix(raw, "/") || strings.ContainsRune(raw, '\x00') ||
		(len(raw) >= 2 && raw[1] == ':') {
		return "", fmt.Errorf("illegal path in archive: %s", name)
	}
	rel := filepath.Clean(filepath.FromSlash(raw))
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" ||
		strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal path in archive: %s", name)
	}
	return rel, nil
}

func findJavaBinary(root string) (string, error) {
	var candidates []string
	exe := "java"
	if runtime.GOOS == "windows" {
		exe = "java.exe"
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Name() == exe {
			// Prefer .../bin/java
			if filepath.Base(filepath.Dir(path)) == "bin" {
				candidates = append([]string{path}, candidates...)
			} else {
				candidates = append(candidates, path)
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no %s under %s", exe, root)
	}
	// Ensure executable bit on unix
	bin := candidates[0]
	if runtime.GOOS != "windows" {
		_ = os.Chmod(bin, 0o755)
	}
	return bin, nil
}
