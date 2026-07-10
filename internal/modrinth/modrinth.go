// Package modrinth resolves Modrinth modpacks for Pastel install / pack pins.
package modrinth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	APIBase   = "https://api.modrinth.com/v2"
	UserAgent = "Pastel/0.1 (+https://kaf.sh)"
)

// Project is a subset of Modrinth project metadata.
type Project struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	ProjectType string `json:"project_type"`
	Description string `json:"description"`
}

// Version is a subset of Modrinth version metadata.
type Version struct {
	ID            string   `json:"id"`
	VersionNumber string   `json:"version_number"`
	Name          string   `json:"name"`
	VersionType   string   `json:"version_type"` // release, beta, alpha
	Loaders       []string `json:"loaders"`
	GameVersions  []string `json:"game_versions"`
	Files         []File   `json:"files"`
}

// File is a downloadable version file.
type File struct {
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Hashes   map[string]string `json:"hashes"`
	Size     int64             `json:"size"`
}

// Pack is a resolved Modrinth modpack version ready to pin/install.
type Pack struct {
	Project Project
	Version Version
	File    File
	// Pin is a stable Pastel pack reference: modrinth:slug or modrinth:slug:version
	Pin string
}

// Client talks to the Modrinth API.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	APIBase   string
}

// New returns a Client with defaults.
func New() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 60 * time.Second},
		UserAgent: UserAgent,
		APIBase:   APIBase,
	}
}

// GetProject fetches a project by id or slug.
func (c *Client) GetProject(idOrSlug string) (*Project, error) {
	idOrSlug = strings.TrimSpace(idOrSlug)
	if idOrSlug == "" {
		return nil, fmt.Errorf("empty project id/slug")
	}
	body, err := c.get(c.APIBase + "/project/" + url.PathEscape(idOrSlug))
	if err != nil {
		return nil, err
	}
	var p Project
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("modrinth project json: %w", err)
	}
	if p.Slug == "" && p.ID == "" {
		return nil, fmt.Errorf("modrinth project not found: %s", idOrSlug)
	}
	return &p, nil
}

// ListVersions returns versions for a project (newest first as returned by API).
func (c *Client) ListVersions(idOrSlug string) ([]Version, error) {
	body, err := c.get(c.APIBase + "/project/" + url.PathEscape(idOrSlug) + "/version")
	if err != nil {
		return nil, err
	}
	var vs []Version
	if err := json.Unmarshal(body, &vs); err != nil {
		return nil, fmt.Errorf("modrinth versions json: %w", err)
	}
	return vs, nil
}

// ResolveModpack picks a version of a modpack project and its primary .mrpack file.
// version may be empty (latest), a version id, or a version_number.
func (c *Client) ResolveModpack(idOrSlug, version string) (*Pack, error) {
	p, err := c.GetProject(idOrSlug)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(p.ProjectType, "modpack") {
		return nil, fmt.Errorf("%q is a Modrinth %s, not a modpack", displayProject(p), p.ProjectType)
	}
	versions, err := c.ListVersions(p.ID)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("modpack %s has no versions", displayProject(p))
	}
	v, err := selectVersion(versions, version)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", displayProject(p), err)
	}
	f, err := primaryMrpack(v)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", displayProject(p), v.VersionNumber, err)
	}
	slug := p.Slug
	if slug == "" {
		slug = p.ID
	}
	pin := "modrinth:" + slug
	if version != "" {
		// Pin the exact version the user asked for (id or number).
		pin = "modrinth:" + slug + ":" + version
	}
	// When installing "latest", track the channel by slug only so ./pastel update
	// / refresh can move forward. The resolved version is still used for the download.
	return &Pack{Project: *p, Version: *v, File: *f, Pin: pin}, nil
}

func displayProject(p *Project) string {
	if p.Title != "" {
		return p.Title
	}
	if p.Slug != "" {
		return p.Slug
	}
	return p.ID
}

func selectVersion(versions []Version, want string) (*Version, error) {
	want = strings.TrimSpace(want)
	if want == "" || want == "latest" {
		// Prefer a release with an .mrpack; else first with mrpack.
		for i := range versions {
			if strings.EqualFold(versions[i].VersionType, "release") {
				if _, err := primaryMrpack(&versions[i]); err == nil {
					return &versions[i], nil
				}
			}
		}
		for i := range versions {
			if _, err := primaryMrpack(&versions[i]); err == nil {
				return &versions[i], nil
			}
		}
		return nil, fmt.Errorf("no version with an .mrpack file")
	}
	for i := range versions {
		if versions[i].ID == want || versions[i].VersionNumber == want {
			return &versions[i], nil
		}
	}
	return nil, fmt.Errorf("version %q not found", want)
}

func primaryMrpack(v *Version) (*File, error) {
	var first *File
	for i := range v.Files {
		f := &v.Files[i]
		if !strings.HasSuffix(strings.ToLower(f.Filename), ".mrpack") &&
			!strings.Contains(strings.ToLower(f.URL), ".mrpack") {
			continue
		}
		if first == nil {
			first = f
		}
		if f.Primary {
			return f, nil
		}
	}
	if first != nil {
		return first, nil
	}
	return nil, fmt.Errorf("no .mrpack file on this version")
}

// ParsePageURL extracts project slug and optional version from a modrinth.com URL.
//
//	https://modrinth.com/modpack/aristea
//	https://modrinth.com/modpack/aristea/version/1.2.3
//	https://modrinth.com/project/AABBCCDD
func ParsePageURL(raw string) (slug, version string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", "", false
	}
	host := strings.ToLower(u.Host)
	if host != "modrinth.com" && host != "www.modrinth.com" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// modpack/slug[/version/x] or project/id
	if len(parts) < 2 {
		return "", "", false
	}
	switch strings.ToLower(parts[0]) {
	case "modpack", "mod", "project":
		slug = parts[1]
	default:
		return "", "", false
	}
	if slug == "" {
		return "", "", false
	}
	if len(parts) >= 4 && strings.EqualFold(parts[2], "version") {
		version = parts[3]
	}
	return slug, version, true
}

// ParseRef parses modrinth:slug or modrinth:slug:version.
func ParseRef(raw string) (slug, version string, ok bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), "modrinth:") {
		return "", "", false
	}
	rest := raw[len("modrinth:"):]
	if rest == "" {
		return "", "", false
	}
	// slug may contain only one extra colon for version
	if i := strings.Index(rest, ":"); i >= 0 {
		return rest[:i], rest[i+1:], true
	}
	return rest, "", true
}

// LooksLikeSlug reports whether s could be a Modrinth project slug (not a path/URL/coord).
func LooksLikeSlug(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) < 2 || len(s) > 64 {
		return false
	}
	if strings.ContainsAny(s, "/\\:@") {
		return false
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return false
	}
	// slug charset (approx Modrinth)
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// ParseSlugVersion parses friend shorthands:
//
//	aristea@0.1.4
//	aristea:0.1.4
//
// Not Maven (group:artifact:version has 3+ segments and a dotted group).
// Not modrinth:… (use ParseRef for that).
func ParseSlugVersion(raw string) (slug, version string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return "", "", false
	}
	if strings.HasPrefix(strings.ToLower(raw), "modrinth:") {
		return "", "", false
	}
	// slug@version
	if i := strings.Index(raw, "@"); i > 0 {
		slug, version = raw[:i], raw[i+1:]
		if LooksLikeSlug(slug) && strings.TrimSpace(version) != "" && !strings.Contains(version, "@") {
			return slug, strings.TrimSpace(version), true
		}
		return "", "", false
	}
	// slug:version — exactly one colon, not a Maven coord
	if strings.Count(raw, ":") != 1 {
		return "", "", false
	}
	i := strings.Index(raw, ":")
	slug, version = raw[:i], raw[i+1:]
	if !LooksLikeSlug(slug) || strings.TrimSpace(version) == "" {
		return "", "", false
	}
	// Maven groups look like com.example (contain a dot) and need artifact too —
	// a single "com.foo:bar" is ambiguous; treat as Modrinth only if slug has no domain-like multi-dot group.
	// Prefer: if first segment has a dot AND second has no leading digit, leave for Maven-ish handling.
	if strings.Contains(slug, ".") && !versionLooksLikeModpackVersion(version) {
		return "", "", false
	}
	return slug, strings.TrimSpace(version), true
}

func versionLooksLikeModpackVersion(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	// version numbers / ids are typically alnum, dots, hyphens, plus (no path)
	if strings.ContainsAny(v, "/\\") {
		return false
	}
	return true
}

func (c *Client) get(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	ua := c.UserAgent
	if ua == "" {
		ua = UserAgent
	}
	req.Header.Set("User-Agent", ua)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not found: %s", path.Base(rawURL))
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", rawURL, res.Status)
	}
	return body, nil
}
