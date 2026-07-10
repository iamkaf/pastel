package author

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iamkaf/pastel/internal/maven"
)

// PublishOptions uploads a built pack directory to Kaf Maven.
type PublishOptions struct {
	Dir        string
	PublishURL string // default https://z.kaf.sh
	PublicBase string // default https://maven.kaf.sh
	Username   string
	Password   string
	DryRun     bool
}

// PublishResult lists uploaded keys.
type PublishResult struct {
	Uploaded []string
	Pin      string // suggested pack = coordinate
}

// Publish uploads pack artifacts from a build output directory (.mrpack + pom + metadata).
func Publish(opt PublishOptions) (*PublishResult, error) {
	if opt.Dir == "" {
		return nil, fmt.Errorf("dir is required")
	}
	if opt.PublishURL == "" {
		opt.PublishURL = maven.PublishBase
	}
	if opt.PublicBase == "" {
		opt.PublicBase = maven.DefaultBase
	}
	if opt.Username == "" {
		opt.Username = os.Getenv("MAVEN_PUBLISH_USERNAME")
	}
	if opt.Password == "" {
		opt.Password = os.Getenv("MAVEN_PUBLISH_PASSWORD")
	}
	if !opt.DryRun && (opt.Username == "" || opt.Password == "") {
		return nil, fmt.Errorf("set MAVEN_PUBLISH_USERNAME and MAVEN_PUBLISH_PASSWORD")
	}

	metaPath := filepath.Join(opt.Dir, "publish.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read publish.json (run pack build first): %w", err)
	}
	var meta struct {
		Group    string `json:"group"`
		Artifact string `json:"artifact"`
		Version  string `json:"version"`
		PackFile string `json:"packFile"`
		POM      string `json:"pom"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	if meta.PackFile == "" {
		return nil, fmt.Errorf("publish.json missing packFile")
	}
	if !strings.HasSuffix(strings.ToLower(meta.PackFile), ".mrpack") {
		return nil, fmt.Errorf("packFile must be a .mrpack (got %s)", meta.PackFile)
	}
	if err := validateMavenGroup(meta.Group); err != nil {
		return nil, err
	}
	if err := validateMavenPart("artifact", meta.Artifact, false); err != nil {
		return nil, err
	}
	if err := validateMavenPart("version", meta.Version, true); err != nil {
		return nil, err
	}
	if err := validatePublishURL(opt.PublishURL); err != nil {
		return nil, err
	}

	groupPath := strings.ReplaceAll(meta.Group, ".", "/")
	versionPrefix := fmt.Sprintf("%s/%s/%s", groupPath, meta.Artifact, meta.Version)
	artifactPrefix := fmt.Sprintf("%s/%s", groupPath, meta.Artifact)

	type item struct {
		local string
		key   string
	}
	var items []item
	add := func(name, key string) error {
		if name == "" {
			return nil
		}
		if filepath.Base(name) != name || name == "." || name == ".." {
			return fmt.Errorf("publish file %q must be a filename inside the build directory", name)
		}
		local := filepath.Join(opt.Dir, name)
		st, err := os.Lstat(local)
		if err != nil {
			return fmt.Errorf("publish file %q: %w", name, err)
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("publish file %q must be a regular file", name)
		}
		items = append(items, item{local: local, key: key})
		if checksumInfo, err := os.Lstat(local + ".sha512"); err == nil {
			if !checksumInfo.Mode().IsRegular() {
				return fmt.Errorf("publish checksum %q must be a regular file", filepath.Base(local)+".sha512")
			}
			items = append(items, item{local: local + ".sha512", key: key + ".sha512"})
		}
		return nil
	}

	if err := add(meta.PackFile, versionPrefix+"/"+meta.PackFile); err != nil {
		return nil, err
	}
	if err := add(meta.POM, versionPrefix+"/"+meta.POM); err != nil {
		return nil, err
	}
	metaXML := filepath.Join(opt.Dir, "maven-metadata.xml")
	if info, err := os.Lstat(metaXML); err == nil && info.Mode().IsRegular() {
		items = append(items, item{local: metaXML, key: artifactPrefix + "/maven-metadata.xml"})
	}

	res := &PublishResult{
		Pin: fmt.Sprintf("%s:%s:%s", meta.Group, meta.Artifact, meta.Version),
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	for _, it := range items {
		url := strings.TrimRight(opt.PublishURL, "/") + "/releases/" + it.key
		if opt.DryRun {
			res.Uploaded = append(res.Uploaded, "DRY "+url)
			continue
		}
		if err := putFile(client, url, it.local, opt.Username, opt.Password); err != nil {
			return res, fmt.Errorf("upload %s: %w", it.key, err)
		}
		res.Uploaded = append(res.Uploaded, it.key)
	}
	return res, nil
}

func validatePublishURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("publish URL is invalid")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("publish URL must use HTTPS so credentials are not exposed")
	}
	return nil
}

func putFile(client *http.Client, url, path, user, pass string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, url, f)
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, pass)
	req.ContentLength = st.Size()
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", "Pastel-pack-publish/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("%s already exists (immutable) — bump the pack version", url)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s: %s", url, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
