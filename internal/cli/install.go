package cli

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/iamkaf/pastel/internal/config"
	"github.com/iamkaf/pastel/internal/modrinth"
	"github.com/iamkaf/pastel/internal/pack"
	"github.com/iamkaf/pastel/internal/ui"
)

// cmdInstall acquires a pack pin, writes server.pastel, and refreshes files.
//
//	./pastel install https://…/pack.mrpack
//	./pastel install https://modrinth.com/modpack/aristea
//	./pastel install aristea
//	./pastel install modrinth:aristea
//	./pastel install com.iamkaf.modpacks:forever-world:1.1.0 -repo https://maven.kaf.sh
func cmdInstall(args []string) error {
	// Allow flags before or after the pack target (friends type either way).
	flags, positional := splitFlags(args)
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	memory := fs.String("memory", "4G", "server memory (-Xmx)")
	repo := fs.String("repo", "", "Maven repository base (comma-separated for several)")
	dir := fs.String("dir", "", "server folder (default: current directory)")
	yes := fs.Bool("yes", false, "overwrite existing server.pastel without asking")
	noRefresh := fs.Bool("no-refresh", false, "only write server.pastel; do not download yet")
	version := fs.String("version", "", "Modrinth version id or version number (optional)")
	if err := fs.Parse(flags); err != nil {
		return err
	}
	if len(positional) < 1 || strings.TrimSpace(positional[0]) == "" {
		printInstallHelp()
		return softError{reason: "install needs a modpack"}
	}
	target := strings.TrimSpace(positional[0])

	ui.Banner()
	ui.Blank()
	ui.Title("Install pack")

	root := *dir
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		root = cwd
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}

	cfgPath := filepath.Join(root, config.DefaultFileName)
	if st, err := os.Stat(cfgPath); err == nil && !st.IsDir() && !*yes {
		ui.Warn("This folder already has a " + ui.Blue("server.pastel") + ".")
		ui.Detail(cfgPath)
		ok, err := confirmYesNo("Replace the pack pin and reinstall? " + ui.Pink("[y/N]") + " ")
		if err != nil {
			return err
		}
		if !ok {
			ui.Info("Cancelled — no changes made.")
			return nil
		}
	}

	// Block mid-run reinstall
	if err := requireServerStopped(root); err != nil {
		return err
	}

	ui.Step("Figuring out " + ui.Pink(target) + "…")
	acq, err := acquirePack(target, *version, splitRepos(*repo))
	if err != nil {
		return err
	}
	ui.OK(acq.Title)
	if acq.Version != "" {
		ui.Detail("version " + acq.Version)
	}
	ui.Detail("pin " + acq.Pin)
	if len(acq.Repositories) > 0 {
		ui.Detail("repos " + strings.Join(acq.Repositories, ", "))
	}

	if err := config.Write(config.WriteOptions{
		Path:         cfgPath,
		Pack:         acq.Pin,
		Memory:       *memory,
		Repositories: acq.Repositories,
	}); err != nil {
		return fmt.Errorf("write server.pastel: %w", err)
	}
	ui.OK("Wrote " + ui.Blue("server.pastel"))

	if *noRefresh {
		ui.Blank()
		ui.Step("Next: " + ui.Blue("./pastel refresh") + " then " + ui.Blue("./pastel run"))
		return nil
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	cf := &commonFlags{config: cfgPath, prune: true}
	resolved, err := loadPack(cfg)
	if err != nil {
		return fmt.Errorf("couldn't load the modpack: %w", err)
	}
	ui.Blank()
	ui.Step(fmt.Sprintf("Downloading %s…", ui.Pink(resolved.Manifest.Name)))
	res, err := doSync(cf, cfg, resolved)
	if err != nil {
		return fmt.Errorf("install failed: %w", err)
	}
	next := "Start the server with " + ui.Blue("./pastel run") + ", then " + ui.Blue("./pastel console") + " for the live log."
	printSyncSummary(res, false, resolved.Manifest.Name, resolved.Manifest.Version, next)
	ui.BigOK("Pack installed")
	return nil
}

// acquired is the result of turning a human install target into a pin.
type acquired struct {
	Pin          string
	Title        string
	Version      string
	Repositories []string
}

func acquirePack(target, versionFlag string, repos []string) (*acquired, error) {
	target = strings.TrimSpace(target)

	// 1) modrinth:slug[:version]
	if slug, ver, ok := modrinth.ParseRef(target); ok {
		if versionFlag != "" {
			ver = versionFlag
		}
		return acquireModrinth(slug, ver)
	}

	// 2) https://modrinth.com/modpack/…
	if slug, ver, ok := modrinth.ParsePageURL(target); ok {
		if versionFlag != "" {
			ver = versionFlag
		}
		return acquireModrinth(slug, ver)
	}

	// 3) Direct .mrpack URL
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
		if looksLikeMrpackURL(target) {
			name := filepath.Base(target)
			if u, err := url.Parse(target); err == nil {
				name = filepath.Base(u.Path)
			}
			return &acquired{Pin: target, Title: name}, nil
		}
		return nil, fmt.Errorf("that URL doesn't look like a .mrpack or a Modrinth modpack page")
	}

	// 4) Local path
	if st, err := os.Stat(target); err == nil && !st.IsDir() {
		abs, err := filepath.Abs(target)
		if err != nil {
			return nil, err
		}
		return &acquired{Pin: abs, Title: filepath.Base(abs)}, nil
	}
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		if _, err := os.Stat(filepath.Join(target, "modrinth.index.json")); err == nil {
			abs, err := filepath.Abs(target)
			if err != nil {
				return nil, err
			}
			return &acquired{Pin: abs, Title: filepath.Base(abs)}, nil
		}
	}

	// 5) Maven coordinate (group:artifact:version) — before slug:version shorthand
	if isMavenCoord(target) {
		if len(repos) == 0 {
			return nil, fmt.Errorf("Maven pack %q needs -repo https://… (no default host)", target)
		}
		// Probe so we fail before writing config
		_, err := pack.Resolve(pack.ResolveSpec{Raw: target, Repositories: repos})
		if err != nil {
			return nil, err
		}
		return &acquired{Pin: target, Title: target, Repositories: repos}, nil
	}

	// 6) Friend shorthand: aristea@0.1.4 or aristea:0.1.4
	if slug, ver, ok := modrinth.ParseSlugVersion(target); ok {
		if versionFlag != "" {
			ver = versionFlag
		}
		return acquireModrinth(slug, ver)
	}

	// 7) Bare slug → Modrinth modpack (latest)
	if modrinth.LooksLikeSlug(target) {
		return acquireModrinth(target, versionFlag)
	}

	return nil, fmt.Errorf("don't know how to install %q — try a Modrinth slug, modpack page URL, or .mrpack link", target)
}

func acquireModrinth(slug, version string) (*acquired, error) {
	cl := modrinth.New()
	mp, err := cl.ResolveModpack(slug, version)
	if err != nil {
		return nil, err
	}
	title := mp.Project.Title
	if title == "" {
		title = mp.Project.Slug
	}
	// Always store a canonical modrinth: pin (never bare slug:version — loadPack must not
	// treat it as a relative path under the server folder).
	pin := mp.Pin
	if !strings.HasPrefix(strings.ToLower(pin), "modrinth:") {
		pin = modrinth.Pin(mp.Project.Slug, mp.Version.VersionNumber)
	}
	return &acquired{
		Pin:     pin,
		Title:   title,
		Version: mp.Version.VersionNumber,
	}, nil
}

func looksLikeMrpackURL(raw string) bool {
	low := strings.ToLower(raw)
	if strings.Contains(low, ".mrpack") {
		return true
	}
	// some CDNs omit extension in path but set content-type later — still require hint
	return false
}

func splitRepos(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func printInstallHelp() {
	ui.Banner()
	ui.Blank()
	ui.Title("Install a modpack")
	ui.Info(ui.Brand() + " will write " + ui.Blue("server.pastel") + " and download the pack into this folder.")
	ui.Blank()
	ui.Step("Pick one of these:")
	ui.Blank()
	fmt.Fprintln(ui.Out, "  "+ui.Blue("./pastel install aristea"))
	ui.Detail("Modrinth modpack (latest)")
	fmt.Fprintln(ui.Out, "  "+ui.Blue("./pastel install aristea:0.1.4")+"   or   "+ui.Blue("aristea@0.1.4"))
	ui.Detail("Specific version")
	fmt.Fprintln(ui.Out, "  "+ui.Blue("./pastel install https://modrinth.com/modpack/aristea"))
	ui.Detail("Modrinth page link")
	fmt.Fprintln(ui.Out, "  "+ui.Blue("./pastel install https://…/pack.mrpack"))
	ui.Detail("Direct pack file")
	fmt.Fprintln(ui.Out, "  "+ui.Blue("./pastel install com.iamkaf.modpacks:forever-world:1.1.0 -repo https://maven.kaf.sh"))
	ui.Detail("Maven coordinate (needs -repo)")
	ui.Blank()
	ui.Info("Then:  " + ui.Blue("./pastel run") + "  →  " + ui.Blue("./pastel console"))
	ui.Blank()
	ui.Detail("Optional: -memory 4G  ·  -version 1.2.3  ·  -yes  ·  -no-refresh")
}

// splitFlags pulls -flag / --flag / -flag=value out of args so flags may follow the pack target.
func splitFlags(args []string) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			// -flag value (not -flag=value and not boolean-only names we know)
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				name := strings.TrimLeft(a, "-")
				// flags that take a value
				switch name {
				case "memory", "repo", "dir", "version", "config":
					i++
					flags = append(flags, args[i])
				}
			}
			continue
		}
		positional = append(positional, a)
	}
	return flags, positional
}
