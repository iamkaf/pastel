package modrinth

import (
	"fmt"
	"strings"
)

// CheckResult is whether a newer Modrinth pack version is available.
type CheckResult struct {
	Slug            string
	PinnedVer       string // empty = tracking channel
	InstalledVer    string
	LatestVer       string // version_number
	LatestID        string
	LatestName      string // project title
	LatestMC        string // best-effort game version label
	UpdateAvailable bool
}

// CheckUpdate compares baseline (installed pack version) to the latest Modrinth release with an .mrpack.
func (c *Client) CheckUpdate(slug, baselineVer string) (*CheckResult, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, fmt.Errorf("empty modrinth slug")
	}
	p, err := c.GetProject(slug)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(p.ProjectType, "modpack") {
		return nil, fmt.Errorf("%q is not a modpack", displayProject(p))
	}
	versions, err := c.ListVersions(p.ID)
	if err != nil {
		return nil, err
	}
	latest, err := selectVersion(versions, "")
	if err != nil {
		return nil, err
	}
	res := &CheckResult{
		Slug:         firstNonEmpty(p.Slug, p.ID),
		InstalledVer: baselineVer,
		LatestVer:    latest.VersionNumber,
		LatestID:     latest.ID,
		LatestName:   firstNonEmpty(p.Title, p.Slug),
		LatestMC:     primaryGameVersion(latest),
	}
	if baselineVer == "" {
		res.UpdateAvailable = true
		return res, nil
	}
	// Prefer numeric compare; fall back to string inequality.
	if compareVersionLabels(baselineVer, latest.VersionNumber) < 0 {
		res.UpdateAvailable = true
	} else if baselineVer != latest.VersionNumber && baselineVer != latest.ID {
		// Non-dotted labels: treat different-from-latest as update if baseline isn't the latest id/number
		res.UpdateAvailable = true
	}
	return res, nil
}

// ListMrpackVersions returns versions that include an .mrpack (API order, newest first).
func (c *Client) ListMrpackVersions(slug string) (*Project, []Version, error) {
	p, err := c.GetProject(slug)
	if err != nil {
		return nil, nil, err
	}
	if !strings.EqualFold(p.ProjectType, "modpack") {
		return nil, nil, fmt.Errorf("%q is not a modpack", displayProject(p))
	}
	all, err := c.ListVersions(p.ID)
	if err != nil {
		return nil, nil, err
	}
	var out []Version
	for _, v := range all {
		if _, err := primaryMrpack(&v); err == nil {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return p, nil, fmt.Errorf("no .mrpack versions for %s", displayProject(p))
	}
	return p, out, nil
}

// Pin builds modrinth:slug or modrinth:slug:version.
func Pin(slug, version string) string {
	slug = strings.TrimSpace(slug)
	version = strings.TrimSpace(version)
	if version == "" {
		return "modrinth:" + slug
	}
	return "modrinth:" + slug + ":" + version
}

func primaryGameVersion(v *Version) string {
	if v == nil || len(v.GameVersions) == 0 {
		return ""
	}
	// Prefer the last listed (often the newest in the range) for a short label.
	return v.GameVersions[len(v.GameVersions)-1]
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// compareVersionLabels returns -1 if a < b, 0 if equal, 1 if a > b (dotted numeric best-effort).
func compareVersionLabels(a, b string) int {
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
		an, aErr := parseIntPrefix(ai)
		bn, bErr := parseIntPrefix(bi)
		if aErr && bErr {
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

func parseIntPrefix(s string) (int, bool) {
	n := 0
	ok := false
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		ok = true
		n = n*10 + int(r-'0')
	}
	return n, ok
}
