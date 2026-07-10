// Package maven fetches artifacts from a Maven repository layout (Kaf Maven).
package maven

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultBase is Kaf's Maven host. Used only when an operator explicitly opts in
// (e.g. repositories = ["https://maven.kaf.sh"] or pack publish verification).
// Pastel never injects this by default — short coordinates require an explicit list.
const DefaultBase = "https://maven.kaf.sh"

// NormalizeRepositories trims bases, drops empties, and de-duplicates (order preserved).
// Empty input yields an empty slice (no default host).
func NormalizeRepositories(bases []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, b := range bases {
		b = strings.TrimRight(strings.TrimSpace(b), "/")
		if b == "" {
			continue
		}
		if _, ok := seen[b]; ok {
			continue
		}
		seen[b] = struct{}{}
		out = append(out, b)
	}
	return out
}

// ErrNoRepositories is returned when a Maven operation needs a host but none were configured.
var ErrNoRepositories = fmt.Errorf("no Maven repositories configured (set repositories = [\"https://…\"] in server.pastel, or pin pack to a full https://…/.mrpack URL)")

// Coordinate is group:artifact:version with optional classifier.
type Coordinate struct {
	Group      string
	Artifact   string
	Version    string
	Classifier string
	// Extension defaults to "json" for packs when empty at call site; stored as given.
	Extension string
}

// ParseCoordinate parses group:artifact:version or group:artifact:version:classifier.
func ParseCoordinate(s string) (Coordinate, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return Coordinate{}, fmt.Errorf("invalid maven coordinate %q (want group:artifact:version[:classifier])", s)
	}
	c := Coordinate{
		Group:    parts[0],
		Artifact: parts[1],
		Version:  parts[2],
	}
	if len(parts) == 4 {
		c.Classifier = parts[3]
	}
	if c.Group == "" || c.Artifact == "" || c.Version == "" {
		return Coordinate{}, fmt.Errorf("invalid maven coordinate %q", s)
	}
	return c, nil
}

// Path returns the repository-relative path for an artifact file.
func (c Coordinate) Path(ext string) string {
	if ext == "" {
		ext = c.Extension
	}
	if ext == "" {
		ext = "jar"
	}
	groupPath := strings.ReplaceAll(c.Group, ".", "/")
	name := c.Artifact + "-" + c.Version
	if c.Classifier != "" {
		name += "-" + c.Classifier
	}
	name += "." + ext
	return fmt.Sprintf("%s/%s/%s/%s", groupPath, c.Artifact, c.Version, name)
}

// URL joins base and coordinate path.
func (c Coordinate) URL(base, ext string) string {
	base = strings.TrimRight(base, "/")
	return base + "/" + c.Path(ext)
}

// Client performs GETs against one or more Maven repos (ordered fallback).
type Client struct {
	// Bases is the ordered repository list (never empty after NewClient).
	Bases     []string
	HTTP      *http.Client
	UserAgent string
}

// NewClient returns a client. bases is an ordered list tried until one succeeds.
// Empty bases is allowed for construction, but Fetch/ListVersions return ErrNoRepositories.
func NewClient(bases ...string) *Client {
	return &Client{
		Bases:     NormalizeRepositories(bases),
		HTTP:      &http.Client{Timeout: 120 * time.Second},
		UserAgent: "Pastel/0.1 (+https://kaf.sh)",
	}
}

// Base returns the primary (first) repository URL, or "" if none.
func (cl *Client) Base() string {
	if cl == nil || len(cl.Bases) == 0 {
		return ""
	}
	return cl.Bases[0]
}

// Fetch downloads an artifact body from the first repository that has it.
func (cl *Client) Fetch(c Coordinate, ext string) ([]byte, error) {
	if len(cl.Bases) == 0 {
		return nil, ErrNoRepositories
	}
	var last error
	for _, base := range cl.Bases {
		data, err := cl.getBytes(c.URL(base, ext))
		if err != nil {
			last = err
			continue
		}
		return data, nil
	}
	return nil, last
}

// FetchPackJSON downloads a modpack manifest. Deprecated name; prefer FetchPack.
func (cl *Client) FetchPackJSON(c Coordinate) ([]byte, error) {
	return cl.FetchPack(c)
}

// FetchPack downloads pack bytes. Packs are published as .mrpack only.
func (cl *Client) FetchPack(c Coordinate) ([]byte, error) {
	data, err := cl.Fetch(c, "mrpack")
	if err != nil {
		return nil, fmt.Errorf("fetch pack .mrpack: %w", err)
	}
	return data, nil
}

// PublishBase is the authenticated upload host (Worker).
const PublishBase = "https://z.kaf.sh"

// LatestVersion reads maven-metadata.xml and returns the release or latest version.
func (cl *Client) LatestVersion(group, artifact string) (string, error) {
	vs, release, err := cl.ListVersions(group, artifact)
	if err != nil {
		return "", err
	}
	if release != "" {
		return release, nil
	}
	if len(vs) == 0 {
		return "", fmt.Errorf("no versions for %s:%s", group, artifact)
	}
	return vs[0], nil // ListVersions returns newest-first
}

// ListVersions returns all versions (newest first) and the release pointer if set.
// Tries each repository until maven-metadata.xml is found.
func (cl *Client) ListVersions(group, artifact string) (versions []string, release string, err error) {
	if len(cl.Bases) == 0 {
		return nil, "", ErrNoRepositories
	}
	groupPath := strings.ReplaceAll(group, ".", "/")
	var last error
	var lastURL string
	for _, base := range cl.Bases {
		u := fmt.Sprintf("%s/%s/%s/maven-metadata.xml", base, groupPath, artifact)
		lastURL = u
		body, err := cl.getBytes(u)
		if err != nil {
			last = err
			continue
		}
		var meta metadata
		if err := xml.Unmarshal(body, &meta); err != nil {
			return nil, "", fmt.Errorf("parse maven-metadata: %w", err)
		}
		release = meta.Versioning.Release
		if release == "" {
			release = meta.Versioning.Latest
		}
		raw := append([]string{}, meta.Versioning.Versions.Version...)
		if len(raw) == 0 {
			if release != "" {
				return []string{release}, release, nil
			}
			last = fmt.Errorf("no versions in %s", u)
			continue
		}
		sort.SliceStable(raw, func(i, j int) bool {
			return versionGreater(raw[i], raw[j])
		})
		return raw, release, nil
	}
	if last == nil {
		last = fmt.Errorf("no maven-metadata at %s", lastURL)
	}
	return nil, "", last
}

func versionGreater(a, b string) bool {
	// local compare without importing pack (avoid cycle)
	return compareDotted(a, b) > 0
}

func compareDotted(a, b string) int {
	a = strings.TrimPrefix(strings.TrimSpace(a), "v")
	b = strings.TrimPrefix(strings.TrimSpace(b), "v")
	if a == b {
		return 0
	}
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var ai, bi string
		if i < len(as) {
			ai = as[i]
		}
		if i < len(bs) {
			bi = bs[i]
		}
		ai = strings.Split(ai, "-")[0]
		bi = strings.Split(bi, "-")[0]
		an, aErr := strconv.Atoi(ai)
		bn, bErr := strconv.Atoi(bi)
		if aErr == nil && bErr == nil {
			if an < bn {
				return -1
			}
			if an > bn {
				return 1
			}
			continue
		}
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

func (cl *Client) getBytes(rawURL string) ([]byte, error) {
	if _, err := url.Parse(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if cl.UserAgent != "" {
		req.Header.Set("User-Agent", cl.UserAgent)
	}
	res, err := cl.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil, fmt.Errorf("GET %s: %s", rawURL, res.Status)
	}
	const maxArtifactBytes = 512 << 20
	if res.ContentLength > maxArtifactBytes {
		return nil, fmt.Errorf("GET %s: artifact is too large", rawURL)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArtifactBytes {
		return nil, fmt.Errorf("GET %s: artifact is too large", rawURL)
	}
	return data, nil
}

type metadata struct {
	Versioning struct {
		Latest   string `xml:"latest"`
		Release  string `xml:"release"`
		Versions struct {
			Version []string `xml:"version"`
		} `xml:"versions"`
	} `xml:"versioning"`
}

// ArtifactFileName builds the standard Maven file name.
func ArtifactFileName(artifact, version, classifier, ext string) string {
	name := artifact + "-" + version
	if classifier != "" {
		name += "-" + classifier
	}
	if ext == "" {
		ext = "jar"
	}
	return name + "." + ext
}
