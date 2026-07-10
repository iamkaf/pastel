package pack

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iamkaf/pastel/internal/maven"
	"github.com/iamkaf/pastel/internal/modrinth"
)

// ResolveSpec is where to load a pack from.
type ResolveSpec struct {
	Raw string
	// Repositories is an ordered Maven base URL list for short coordinates.
	// Empty defaults to Kaf Maven.
	Repositories []string
	// CacheDir stores remote .mrpack downloads (e.g. server/.pastel/cache/packs).
	CacheDir string
	// Side filters mrpack env (default server).
	Side string
}

// Resolved is a pack ready for sync.
type Resolved struct {
	Manifest   *Manifest
	Coordinate string
	// Format is "mrpack" or "pastel".
	Format string
	// Mrpack is set when overrides can be applied from a zip or directory.
	Mrpack *LoadedMrpack
}

// Resolve loads a pack from a Maven coordinate, file path, file: URL, or https URL.
// Preferred pack format is Modrinth .mrpack; legacy Pastel JSON remains supported.
func Resolve(spec ResolveSpec) (*Resolved, error) {
	raw := strings.TrimSpace(spec.Raw)
	if raw == "" {
		return nil, fmt.Errorf("empty pack reference")
	}
	side := spec.Side
	if side == "" {
		side = SideServer
	}

	if strings.HasPrefix(raw, "file:") {
		path := stripFileURL(raw)
		return resolvePath(path, "file:"+path, side)
	}

	if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
		// Modrinth project page → resolve to latest/pinned .mrpack
		if slug, ver, ok := modrinth.ParsePageURL(raw); ok {
			return resolveModrinth(slug, ver, "modrinth:"+slug, spec.CacheDir, side)
		}
		return resolveURL(raw, spec.CacheDir, side)
	}

	if slug, ver, ok := modrinth.ParseRef(raw); ok {
		pin := raw
		return resolveModrinth(slug, ver, pin, spec.CacheDir, side)
	}

	// Friend shorthands in server.pastel: aristea:0.1.4 / aristea@0.1.4
	if slug, ver, ok := modrinth.ParseSlugVersion(raw); ok {
		return resolveModrinth(slug, ver, modrinth.Pin(slug, ver), spec.CacheDir, side)
	}

	if isCoordinate(raw) {
		return resolveMaven(raw, spec.Repositories, spec.CacheDir, side)
	}

	// Bare Modrinth slug as pack pin
	if modrinth.LooksLikeSlug(raw) {
		return resolveModrinth(raw, "", modrinth.Pin(raw, ""), spec.CacheDir, side)
	}

	// Relative/absolute path
	path := raw
	if _, err := os.Stat(path); err == nil {
		return resolvePath(path, "file:"+path, side)
	}

	return nil, fmt.Errorf("unrecognized pack reference %q (want .mrpack path/URL, modrinth:slug, slug@version, or Maven coordinate)", raw)
}

func resolveModrinth(slug, version, pin, cacheDir, side string) (*Resolved, error) {
	cl := modrinth.New()
	mp, err := cl.ResolveModpack(slug, version)
	if err != nil {
		return nil, fmt.Errorf("modrinth: %w", err)
	}
	// Download the primary .mrpack via the CDN URL and verify the hash/size
	// returned by Modrinth before parsing or caching it.
	data, err := httpGet(mp.File.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch pack %s: %w", mp.File.URL, err)
	}
	if mp.File.Size > 0 && int64(len(data)) != mp.File.Size {
		return nil, fmt.Errorf("modrinth pack size mismatch (want %d bytes, got %d)", mp.File.Size, len(data))
	}
	if err := verifyModrinthPack(data, mp.File.Hashes); err != nil {
		return nil, err
	}
	res, err := resolveBytes(data, mp.File.URL, cacheDir, side)
	if err != nil {
		return nil, err
	}
	// Keep a friendly pin in state (slug channel or pinned version)
	if pin != "" {
		res.Coordinate = pin
	} else {
		res.Coordinate = mp.Pin
	}
	// Prefer API title/version when index is sparse
	if res.Manifest != nil {
		if res.Manifest.Name == "" {
			res.Manifest.Name = mp.Project.Title
		}
		if res.Manifest.Version == "" {
			res.Manifest.Version = mp.Version.VersionNumber
		}
	}
	return res, nil
}

func verifyModrinthPack(data []byte, hashes map[string]string) error {
	if want := strings.ToLower(strings.TrimSpace(hashes["sha512"])); want != "" {
		got := sha512.Sum512(data)
		if hex.EncodeToString(got[:]) != want {
			return fmt.Errorf("modrinth pack SHA-512 mismatch")
		}
		return nil
	}
	if want := strings.ToLower(strings.TrimSpace(hashes["sha1"])); want != "" {
		got := sha1.Sum(data)
		if hex.EncodeToString(got[:]) != want {
			return fmt.Errorf("modrinth pack SHA-1 mismatch")
		}
		return nil
	}
	return fmt.Errorf("modrinth pack metadata has no supported checksum")
}

func stripFileURL(raw string) string {
	if strings.HasPrefix(raw, "file:///") {
		return raw[len("file://"):]
	}
	if strings.HasPrefix(raw, "file://") {
		return raw[len("file://"):]
	}
	return strings.TrimPrefix(raw, "file:")
}

func resolvePath(path, coord, side string) (*Resolved, error) {
	lower := strings.ToLower(path)
	// Directory or .mrpack or index
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		if _, err := os.Stat(filepath.Join(path, "modrinth.index.json")); err == nil {
			loaded, err := LoadMrpack(path)
			if err != nil {
				return nil, err
			}
			return mrpackResolved(loaded, coord, side), nil
		}
	}
	if strings.HasSuffix(lower, ".mrpack") || strings.HasSuffix(lower, "modrinth.index.json") {
		loaded, err := LoadMrpack(path)
		if err != nil {
			return nil, err
		}
		// Keep zip path for overrides
		if strings.HasSuffix(lower, ".mrpack") && loaded.ZipPath == "" {
			loaded.ZipPath = path
		}
		return mrpackResolved(loaded, coord, side), nil
	}

	// Peek at content
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isZipBytes(data) {
		// Could be mrpack or legacy zip pack
		if loaded, err := loadMrpackZip(path, data); err == nil {
			return mrpackResolved(loaded, coord, side), nil
		}
	}
	if IsMrpackIndexJSON(data) {
		loaded, err := LoadMrpack(path)
		if err != nil {
			return nil, err
		}
		return mrpackResolved(loaded, coord, side), nil
	}

	m, err := DecodeManifestBytes(data)
	if err != nil {
		return nil, err
	}
	return &Resolved{Manifest: m, Coordinate: coord, Format: "pastel"}, nil
}

func resolveURL(raw, cacheDir, side string) (*Resolved, error) {
	data, err := httpGet(raw)
	if err != nil {
		return nil, fmt.Errorf("fetch pack %s: %w", raw, err)
	}
	return resolveBytes(data, raw, cacheDir, side)
}

func resolveMaven(raw string, repositories []string, cacheDir, side string) (*Resolved, error) {
	coord, err := maven.ParseCoordinate(raw)
	if err != nil {
		return nil, err
	}
	repos := maven.NormalizeRepositories(repositories)
	if len(repos) == 0 {
		return nil, fmt.Errorf("%w — pack %q is a Maven coordinate", maven.ErrNoRepositories, raw)
	}
	cl := maven.NewClient(repos...)
	display := raw
	if coord.Version == "latest" {
		ver, err := cl.LatestVersion(coord.Group, coord.Artifact)
		if err != nil {
			return nil, fmt.Errorf("resolve latest: %w", err)
		}
		coord.Version = ver
		display = coord.Group + ":" + coord.Artifact + ":" + ver
		if coord.Classifier != "" {
			display += ":" + coord.Classifier
		}
	}
	data, err := cl.FetchPack(coord)
	if err != nil {
		return nil, fmt.Errorf("fetch pack %s: %w", display, err)
	}
	return resolveBytes(data, display, cacheDir, side)
}

func resolveBytes(data []byte, coord, cacheDir, side string) (*Resolved, error) {
	// mrpack zip
	if isZipBytes(data) {
		if hasMrpackIndex(data) {
			path, err := cachePackBytes(cacheDir, coord, data, ".mrpack")
			if err != nil {
				// Fall back to memory-only index (no overrides from zip path)
				loaded, err2 := DecodeMrpackBytes(data)
				if err2 != nil {
					return nil, err
				}
				return mrpackResolved(loaded, coord, side), nil
			}
			loaded, err := LoadMrpack(path)
			if err != nil {
				return nil, err
			}
			return mrpackResolved(loaded, coord, side), nil
		}
	}
	// bare index
	if IsMrpackIndexJSON(data) {
		loaded, err := DecodeMrpackBytes(data)
		if err != nil {
			return nil, err
		}
		return mrpackResolved(loaded, coord, side), nil
	}
	// legacy Pastel
	m, err := DecodeManifestBytes(data)
	if err != nil {
		return nil, err
	}
	return &Resolved{Manifest: m, Coordinate: coord, Format: "pastel"}, nil
}

func hasMrpackIndex(data []byte) bool {
	loaded, err := DecodeMrpackBytes(data)
	return err == nil && loaded != nil && loaded.Index != nil
}

func mrpackResolved(loaded *LoadedMrpack, coord, side string) *Resolved {
	return &Resolved{
		Manifest:   loaded.ToManifest(side),
		Coordinate: coord,
		Format:     "mrpack",
		Mrpack:     loaded,
	}
}

func cachePackBytes(cacheDir, coord string, data []byte, ext string) (string, error) {
	if cacheDir == "" {
		return "", fmt.Errorf("no cache dir")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:16]) + ext
	// also fingerprint coord lightly for debugging
	_ = coord
	path := filepath.Join(cacheDir, name)
	if st, err := os.Stat(path); err == nil && st.Size() == int64(len(data)) {
		return path, nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

func httpGet(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Pastel/0.1 (+https://kaf.sh)")
	client := &http.Client{Timeout: 10 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", res.Status)
	}
	const maxPackBytes = 512 << 20
	if res.ContentLength > maxPackBytes {
		return nil, fmt.Errorf("pack is too large (%d bytes; limit %d)", res.ContentLength, maxPackBytes)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxPackBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPackBytes {
		return nil, fmt.Errorf("pack is too large (limit %d bytes)", maxPackBytes)
	}
	return data, nil
}

func isCoordinate(s string) bool {
	if strings.Contains(s, "/") || strings.Contains(s, `\`) {
		return false
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return false
	}
	parts := strings.Split(s, ":")
	return len(parts) >= 3 && len(parts) <= 4 && strings.Contains(parts[0], ".")
}
