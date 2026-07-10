// Package sync reconciles a server directory to a pack manifest.
package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/iamkaf/pastel/internal/fetch"
	"github.com/iamkaf/pastel/internal/pack"
	"github.com/iamkaf/pastel/internal/state"
)

// DefaultDownloadWorkers is how many file downloads run at once.
const DefaultDownloadWorkers = 8

// Reporter receives human-friendly sync progress events.
type Reporter interface {
	Unchanged(path string)
	Download(path string)
	WouldDownload(path string)
	WouldUpdate(path string)
	Prune(path string)
	WouldPrune(path string)
}

// Flusher can flush buffered report lines (e.g. compressed prune summary).
type Flusher interface {
	Flush()
}

// Options control a sync run.
type Options struct {
	Root           string
	Manifest       *pack.Manifest
	PackCoordinate string
	// Repositories is an ordered Maven base list for maven: file coordinates.
	Repositories []string
	PruneMods    bool
	DryRun       bool
	Report       Reporter
	// DownloadWorkers limits concurrent downloads (0 = default).
	DownloadWorkers int
	// Mrpack supplies overrides/ and server-overrides/ (optional).
	Mrpack *pack.LoadedMrpack
	// Side for override layers (default server).
	Side string
}

// Result summarizes a sync.
type Result struct {
	Downloaded int
	Unchanged  int
	Bundles    int
	Overrides  int
	Loader     bool // true if a loader jar was installed this run
	Pruned     []string
	ServerJar  string
}

type fileJob struct {
	file pack.File
	dest string
}

// Run reconciles the server root to the manifest.
func Run(opt Options) (*Result, error) {
	if opt.Manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	if err := opt.Manifest.Validate(); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(opt.Root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}

	workers := opt.DownloadWorkers
	if workers <= 0 {
		workers = DefaultDownloadWorkers
	}

	dl := fetch.New(opt.Repositories...)
	res := &Result{}
	wantedMods := map[string]struct{}{}
	wantedRootJars := map[string]struct{}{}
	rep := opt.Report

	var jobs []fileJob
	for _, f := range opt.Manifest.Files {
		rel := filepath.FromSlash(f.Path)
		if strings.HasPrefix(rel, "world"+string(os.PathSeparator)) || rel == "world" {
			return nil, fmt.Errorf("refusing to manage world path %q", f.Path)
		}
		dest := filepath.Join(root, rel)
		slash := filepath.ToSlash(f.Path)
		if strings.HasPrefix(slash, "mods/") {
			wantedMods[filepath.Base(rel)] = struct{}{}
		}
		// Track root-level jars (Fabric launcher, etc.) for orphan cleanup.
		if !strings.Contains(slash, "/") && strings.HasSuffix(strings.ToLower(slash), ".jar") {
			wantedRootJars[filepath.Base(rel)] = struct{}{}
		}

		if opt.DryRun {
			algo, want, _ := f.PreferredHash()
			if st, err := os.Stat(dest); err == nil && !st.IsDir() {
				ok, _ := fetch.FileMatches(dest, algo, want)
				if ok {
					res.Unchanged++
					if rep != nil {
						rep.Unchanged(f.Path)
					}
					continue
				}
				res.Downloaded++
				if rep != nil {
					rep.WouldUpdate(f.Path)
				}
				continue
			}
			res.Downloaded++
			if rep != nil {
				rep.WouldDownload(f.Path)
			}
			continue
		}

		jobs = append(jobs, fileJob{file: f, dest: dest})
	}

	if !opt.DryRun && len(jobs) > 0 {
		if err := downloadParallel(dl, jobs, workers, rep, res); err != nil {
			return res, err
		}
	}

	// Config (and other) bundles — sequential (small, extract-heavy)
	for _, b := range opt.Manifest.Bundles {
		label := "bundle:" + b.ID
		if opt.DryRun {
			res.Bundles++
			if rep != nil {
				rep.WouldUpdate(label)
			}
			continue
		}
		changed, err := dl.EnsureBundle(b, root)
		if err != nil {
			return res, err
		}
		if changed {
			res.Bundles++
			if rep != nil {
				rep.Download(label)
			}
		} else {
			res.Unchanged++
			if rep != nil {
				rep.Unchanged(label)
			}
		}
	}

	// mrpack overrides (overrides/ then server-overrides/)
	side := opt.Side
	if side == "" {
		side = pack.SideServer
	}
	if opt.Mrpack != nil {
		if opt.DryRun {
			res.Overrides++
			if rep != nil {
				rep.WouldUpdate("overrides")
			}
			// Dry-run: still list override mod jars so prune wouldn't remove them in a real run.
			if names, err := opt.Mrpack.ListOverrideModJars(side); err == nil {
				for _, name := range names {
					wantedMods[name] = struct{}{}
				}
			}
		} else {
			written, err := opt.Mrpack.ApplyOverrides(root, side)
			if err != nil {
				return res, fmt.Errorf("mrpack overrides: %w", err)
			}
			if len(written) > 0 {
				res.Overrides = len(written)
				if rep != nil {
					rep.Download(fmt.Sprintf("overrides (%d files)", len(written)))
				}
			}
			// Jars shipped only via overrides/mods/ must not be pruned as "extra".
			for _, name := range pack.OverrideModJars(written) {
				wantedMods[name] = struct{}{}
			}
		}
	}

	// Install loader jar / detect launch from dependencies + on-disk tree.
	if !opt.DryRun {
		changed, err := pack.EnsureLoader(root, opt.Manifest, "")
		if err != nil {
			return res, fmt.Errorf("loader: %w", err)
		}
		if changed {
			res.Loader = true
			if rep != nil && opt.Manifest.Launch != nil && opt.Manifest.Launch.Jar != "" {
				rep.Download(opt.Manifest.Launch.Jar)
			}
		}
		// Track managed root jar for prune
		if opt.Manifest.Launch != nil && opt.Manifest.Launch.Jar != "" {
			wantedRootJars[filepath.Base(opt.Manifest.Launch.Jar)] = struct{}{}
		}
	}

	if opt.PruneMods {
		if err := pruneMods(root, wantedMods, opt.DryRun, rep, res); err != nil {
			return res, err
		}
		if err := pruneRootLaunchers(root, wantedRootJars, opt.Manifest, opt.DryRun, rep, res); err != nil {
			return res, err
		}
	}

	if f, ok := rep.(Flusher); ok {
		f.Flush()
	}

	res.ServerJar = resolveServerJar(root, opt.Manifest)

	if !opt.DryRun {
		st := &state.State{
			PackCoordinate: opt.PackCoordinate,
			PackName:       opt.Manifest.Name,
			PackVersion:    opt.Manifest.Version,
			Minecraft:      opt.Manifest.Minecraft(),
			Loader:         opt.Manifest.LoaderKind(),
			ModCount:       opt.Manifest.ModCount(),
			AppliedAt:      time.Now().UTC(),
			FileCount:      len(opt.Manifest.Files) + len(opt.Manifest.Bundles) + res.Overrides,
			ServerJar:      res.ServerJar,
		}
		if err := state.Save(root, st); err != nil {
			return res, fmt.Errorf("save state: %w", err)
		}
	}
	return res, nil
}

func downloadParallel(dl *fetch.Downloader, jobs []fileJob, workers int, rep Reporter, res *Result) error {
	if workers > len(jobs) {
		workers = len(jobs)
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, j := range jobs {
		j := j
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			mu.Lock()
			if firstErr != nil {
				mu.Unlock()
				return
			}
			mu.Unlock()

			changed, err := dl.EnsureFile(j.file, j.dest)
			mu.Lock()
			defer mu.Unlock()
			if firstErr != nil {
				return
			}
			if err != nil {
				firstErr = err
				return
			}
			if changed {
				res.Downloaded++
				if rep != nil {
					rep.Download(j.file.Path)
				}
			} else {
				res.Unchanged++
				if rep != nil {
					rep.Unchanged(j.file.Path)
				}
			}
		}()
	}
	wg.Wait()
	return firstErr
}

func pruneMods(root string, wanted map[string]struct{}, dry bool, rep Reporter, res *Result) error {
	if len(wanted) == 0 {
		return nil
	}
	modsDir := filepath.Join(root, "mods")
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".jar") {
			continue
		}
		if _, ok := wanted[name]; ok {
			continue
		}
		rel := "mods/" + name
		if dry {
			if rep != nil {
				rep.WouldPrune(rel)
			}
			res.Pruned = append(res.Pruned, rel)
			continue
		}
		if err := os.Remove(filepath.Join(modsDir, name)); err != nil {
			return fmt.Errorf("prune %s: %w", name, err)
		}
		if rep != nil {
			rep.Prune(rel)
		}
		res.Pruned = append(res.Pruned, rel)
	}
	return nil
}

// pruneRootLaunchers removes managed root server jars not declared by the pack
// (old Fabric/Forge/NeoForge/Quilt/vanilla launchers left after upgrades).
func pruneRootLaunchers(root string, wanted map[string]struct{}, m *pack.Manifest, dry bool, rep Reporter, res *Result) error {
	if m.Launch != nil && m.Launch.Jar != "" {
		wanted[filepath.Base(m.Launch.Jar)] = struct{}{}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !pack.IsManagedRootJar(name) {
			continue
		}
		if _, ok := wanted[name]; ok {
			continue
		}
		if dry {
			if rep != nil {
				rep.WouldPrune(name)
			}
			res.Pruned = append(res.Pruned, name)
			continue
		}
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			return fmt.Errorf("prune launcher %s: %w", name, err)
		}
		if rep != nil {
			rep.Prune(name)
		}
		res.Pruned = append(res.Pruned, name)
	}
	return nil
}

// resolveServerJar returns a path for display / legacy jar-mode checks.
// Args-file loaders (NeoForge/Forge) may return "" — Start uses BuildJavaArgs instead.
func resolveServerJar(root string, m *pack.Manifest) string {
	if m.Launch != nil && m.Launch.Jar != "" {
		p := filepath.Join(root, filepath.FromSlash(m.Launch.Jar))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
		p = filepath.Join(root, filepath.Base(m.Launch.Jar))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	// Prefer loader-specific jars, then server.jar
	var fallback string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".jar") {
			continue
		}
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "fabric-server-") || strings.HasPrefix(lower, "quilt-server-") {
			return filepath.Join(root, name)
		}
		if strings.HasPrefix(lower, "neoforge-") || strings.HasPrefix(lower, "forge-") {
			return filepath.Join(root, name)
		}
		if lower == "server.jar" {
			fallback = filepath.Join(root, name)
		}
	}
	return fallback
}
