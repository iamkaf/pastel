// Package cli implements Pastel subcommands with friendly output.
package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/iamkaf/pastel/internal/config"
	"github.com/iamkaf/pastel/internal/jre"
	"github.com/iamkaf/pastel/internal/maven"
	"github.com/iamkaf/pastel/internal/mcprops"
	"github.com/iamkaf/pastel/internal/modrinth"
	"github.com/iamkaf/pastel/internal/pack"
	"github.com/iamkaf/pastel/internal/runtime"
	"github.com/iamkaf/pastel/internal/state"
	"github.com/iamkaf/pastel/internal/sync"
	"github.com/iamkaf/pastel/internal/ui"
)

// Version is set from main.
var Version = "0.1.0-dev"

// Run dispatches subcommands. args should be os.Args[1:].
// Bare `pastel` is the home screen (status + what you can do) — never auto-starts the server.
func Run(args []string) error {
	if len(args) == 0 {
		return friendly(cmdHome(nil))
	}
	switch args[0] {
	case "install", "get", "add":
		return friendly(cmdInstall(args[1:]))
	case "run":
		return friendly(cmdRun(args[1:]))
	case "console", "attach", "logs", "terminal":
		return friendly(cmdConsole(args[1:]))
	case "refresh":
		return friendly(cmdRefresh(args[1:]))
	case "sync": // deprecated alias
		return friendly(cmdRefresh(args[1:]))
	case "status":
		return friendly(cmdStatus(args[1:]))
	case "home", "hi", "hello":
		return friendly(cmdHome(args[1:]))
	case "stop":
		return friendly(cmdStop(args[1:]))
	case "update", "upgrade":
		return friendly(cmdUpdate(args[1:]))
	case "self-update", "selfupdate":
		return friendly(cmdSelfUpdate(args[1:]))
	case "pack":
		return cmdPack(args[1:])
	case "__hold-fifo":
		// Internal: keep console FIFO open for background server.
		if len(args) < 2 {
			return fmt.Errorf("usage: pastel __hold-fifo <path>")
		}
		return runtime.HoldFIFO(args[1])
	case "__supervise":
		// Internal: own the background Java process and restart it after crashes.
		if len(args) < 5 || args[3] != "--" {
			return fmt.Errorf("usage: pastel __supervise <root> <auto-restart> -- <java> [args...]")
		}
		autoRestart, err := strconv.ParseBool(args[2])
		if err != nil {
			return fmt.Errorf("invalid auto-restart value: %w", err)
		}
		return runtime.Supervise(args[1], args[4], args[5:], autoRestart)
	case "version", "-version", "--version":
		ui.Banner()
		ui.Detail("version " + Version)
		return nil
	case "help", "-h", "--help":
		ui.HelpBlock()
		return nil
	default:
		// Flags alone → still home (don't surprise-start the server)
		if strings.HasPrefix(args[0], "-") {
			return friendly(cmdHome(args))
		}
		return friendly(fmt.Errorf("I don't know the command %q — try: ./pastel  (or ./pastel help)", args[0]))
	}
}

func friendly(err error) error {
	if err == nil {
		return nil
	}
	// softError was already explained in plain language — don't scare the user.
	if _, ok := err.(softError); ok {
		return errSilent{err}
	}
	// Runtime already printed a friend-facing crash/help block.
	var explained *runtime.ExplainedError
	if errors.As(err, &explained) {
		return errSilent{err}
	}
	msg := err.Error()
	ui.Blank()
	ui.ErrorMessage("Something went wrong", msg, tipsFor(msg)...)
	return errSilent{err}
}

// errSilent is an error main should not re-print.
type errSilent struct{ err error }

func (e errSilent) Error() string { return e.err.Error() }
func (e errSilent) Unwrap() error { return e.err }

// softError means we already printed a friendly explanation (not a crash).
type softError struct{ reason string }

func (e softError) Error() string { return e.reason }

// IsSilent reports whether main should skip printing the error again.
func IsSilent(err error) bool {
	_, ok := err.(errSilent)
	return ok
}

func tipsFor(msg string) []string {
	low := strings.ToLower(msg)
	var tips []string
	if strings.Contains(low, "server.pastel") || strings.Contains(low, "no server.pastel") {
		tips = append(tips, "First time?  ./pastel install <modpack-slug-or-url>")
	}
	if strings.Contains(low, "no maven repositories") {
		tips = append(tips, "Add repositories = [\"https://…\"] to server.pastel, or pin an https://…/.mrpack URL")
	}
	if strings.Contains(low, "not a modpack") || strings.Contains(low, "modpack not found") || strings.Contains(low, "not found") {
		tips = append(tips, "Check the Modrinth slug or page URL for the modpack")
	}
	if strings.Contains(low, "java") {
		tips = append(tips, "Install Java and make sure the java command works in Terminal")
	}
	if strings.Contains(low, "get ") || strings.Contains(low, "fetch") || strings.Contains(low, "download") {
		tips = append(tips, "Check your internet connection and try again")
	}
	if len(tips) == 0 {
		tips = append(tips, "Run ./pastel for a friendly overview")
	}
	return tips
}

type commonFlags struct {
	config  string
	root    string
	dryRun  bool
	prune   bool
	verbose bool
}

func parseCommon(args []string, withDryRun bool) (*commonFlags, []string, error) {
	fs := flag.NewFlagSet("pastel", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	c := &commonFlags{prune: true}
	fs.StringVar(&c.config, "config", "", "path to server.pastel")
	fs.StringVar(&c.root, "root", "", "server root directory")
	fs.BoolVar(&c.verbose, "v", false, "verbose logs")
	if withDryRun {
		fs.BoolVar(&c.dryRun, "dry-run", false, "plan only")
		fs.BoolVar(&c.prune, "prune", true, "prune unlisted jars under mods/")
		noPrune := fs.Bool("no-prune", false, "do not prune extra mods")
		if err := fs.Parse(args); err != nil {
			return nil, nil, err
		}
		if *noPrune {
			c.prune = false
		}
		return c, fs.Args(), nil
	}
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	return c, fs.Args(), nil
}

func loadInstance(c *commonFlags) (*config.Config, error) {
	path := c.config
	var err error
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		path, err = config.Find(cwd)
		if err != nil {
			return nil, fmt.Errorf("couldn't find server.pastel in this folder")
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if c.root != "" {
		cfg.ServerDir = c.root
	}
	return cfg, nil
}

func loadPack(cfg *config.Config) (*pack.Resolved, error) {
	raw := strings.TrimSpace(cfg.Pack)
	// Only join relative filesystem paths to the server.pastel directory.
	// Never path-join scheme pins (modrinth:, Maven coords, http(s), slug:ver, etc.).
	if packRefIsRelativePath(raw) && !filepath.IsAbs(raw) {
		raw = filepath.Join(filepath.Dir(cfg.Path()), raw)
	}
	cache := filepath.Join(cfg.Root(), ".pastel", "cache", "packs")
	return pack.Resolve(pack.ResolveSpec{
		Raw:          raw,
		Repositories: cfg.MavenRepositories(),
		CacheDir:     cache,
		Side:         pack.SideServer,
	})
}

// packRefIsRelativePath is true only for local file/dir pins that may be relative.
func packRefIsRelativePath(raw string) bool {
	if raw == "" {
		return false
	}
	low := strings.ToLower(raw)
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "file:") {
		return false
	}
	if strings.HasPrefix(low, "modrinth:") {
		return false
	}
	if isMavenCoord(raw) {
		return false
	}
	// Friend shorthands that must not become server/aristea:0.1.4
	if _, _, ok := modrinth.ParseSlugVersion(raw); ok {
		return false
	}
	if modrinth.LooksLikeSlug(raw) {
		return false
	}
	return true
}

func isMavenCoord(s string) bool {
	if strings.Contains(s, "/") || strings.Contains(s, `\`) || strings.HasPrefix(s, "file:") {
		return false
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return false
	}
	if strings.HasPrefix(strings.ToLower(s), "modrinth:") {
		return false
	}
	parts := strings.Split(s, ":")
	return len(parts) >= 3 && len(parts) <= 4 && strings.Contains(parts[0], ".")
}

func doSync(cf *commonFlags, cfg *config.Config, res *pack.Resolved) (*sync.Result, error) {
	return sync.Run(sync.Options{
		Root:           cfg.Root(),
		Manifest:       res.Manifest,
		PackCoordinate: res.Coordinate,
		Repositories:   cfg.MavenRepositories(),
		PruneMods:      cf.prune,
		DryRun:         cf.dryRun,
		Report:         ui.NewSyncReport(cf.verbose),
		Mrpack:         res.Mrpack,
		Side:           pack.SideServer,
	})
}

// printSyncSummary prints the result box. nextStep is shown after a real apply (not dry-run).
func printSyncSummary(res *sync.Result, dry bool, name, version, nextStep string) {
	ui.Blank()
	if dry {
		ui.Title("Preview")
	} else {
		ui.Title("All set")
	}
	lines := []string{
		fmt.Sprintf("%s  %s", ui.Pink(name), ui.Blue("v"+version)),
	}
	if res.Downloaded > 0 {
		if dry {
			lines = append(lines, fmt.Sprintf("%s file(s) would be updated", ui.Bold(fmt.Sprint(res.Downloaded))))
		} else {
			lines = append(lines, fmt.Sprintf("%s file(s) downloaded or updated", ui.Bold(fmt.Sprint(res.Downloaded))))
		}
	} else if res.Bundles == 0 {
		lines = append(lines, "Everything was already up to date")
	}
	if res.Bundles > 0 {
		if dry {
			lines = append(lines, fmt.Sprintf("%s config/bundle(s) would refresh", ui.Bold(fmt.Sprint(res.Bundles))))
		} else {
			lines = append(lines, fmt.Sprintf("%s config/bundle(s) applied", ui.Bold(fmt.Sprint(res.Bundles))))
		}
	}
	if res.Overrides > 0 {
		if dry {
			lines = append(lines, "overrides would apply")
		} else {
			lines = append(lines, fmt.Sprintf("%s override file(s) applied", ui.Bold(fmt.Sprint(res.Overrides))))
		}
	}
	if res.Loader {
		lines = append(lines, "loader installed")
	}
	if res.Unchanged > 0 {
		lines = append(lines, fmt.Sprintf("%d item(s) already good", res.Unchanged))
	}
	if n := len(res.Pruned); n > 0 {
		if dry {
			lines = append(lines, fmt.Sprintf("%d extra mod(s) would be removed", n))
		} else {
			lines = append(lines, fmt.Sprintf("%d extra mod(s) removed", n))
		}
	}
	ui.SummaryBox(lines...)
	if !dry && nextStep != "" {
		ui.Blank()
		ui.Title("Next step")
		ui.Step(nextStep)
	}
}

func requireServerStopped(root string) error {
	pid, alive, err := runtime.Running(root)
	if err != nil {
		return err
	}
	if !alive {
		return nil
	}
	ui.Blank()
	ui.Title("Hold on — the server is running")
	if pid > 0 {
		ui.Detail(fmt.Sprintf("process %d", pid))
	}
	ui.Info("Pastel won't change mods or configs while the game is up.")
	ui.Info("That can break the world for anyone who's playing.")
	ui.Blank()
	ui.Step("Stop the server first:  " + ui.Blue("./pastel stop"))
	if pid > 0 {
		ui.Detail("If that says it's not running:  " + ui.Blue(fmt.Sprintf("./pastel stop -pid %d", pid)))
	}
	ui.Detail("Deleted the folder while it was up?  " + ui.Blue("./pastel stop -orphans"))
	ui.Detail("Then run your command again.")
	return softError{reason: "server is running"}
}

func cmdRefresh(args []string) error {
	cf, _, err := parseCommon(args, true)
	if err != nil {
		return err
	}
	ui.Banner()
	ui.Blank()
	cfg, err := loadInstance(cf)
	if err != nil {
		return err
	}
	if err := requireServerStopped(cfg.Root()); err != nil {
		return err
	}
	resolved, err := loadPack(cfg)
	if err != nil {
		return fmt.Errorf("couldn't load the modpack: %w", err)
	}
	manifest := resolved.Manifest
	if cf.dryRun {
		ui.Step(fmt.Sprintf("Checking %s %s (preview only)…", ui.Pink(manifest.Name), ui.Blue("v"+manifest.Version)))
	} else {
		ui.Step(fmt.Sprintf("Refreshing %s %s…", ui.Pink(manifest.Name), ui.Blue("v"+manifest.Version)))
	}
	ui.Detail(resolved.Coordinate)
	if resolved.Format == "mrpack" {
		ui.Detail("format: mrpack")
	}
	res, err := doSync(cf, cfg, resolved)
	if err != nil {
		return fmt.Errorf("refresh failed: %w", err)
	}
	next := ""
	if !cf.dryRun {
		next = "Start the server with " + ui.Blue("./pastel run") + ", then " + ui.Blue("./pastel console") + " for the live log."
	}
	printSyncSummary(res, cf.dryRun, manifest.Name, manifest.Version, next)
	return nil
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	foreground := fs.Bool("foreground", false, "keep the server attached to this terminal")
	fgShort := fs.Bool("f", false, "short for -foreground")
	configPath := fs.String("config", "", "path to server.pastel")
	verbose := fs.Bool("v", false, "verbose")
	noPrune := fs.Bool("no-prune", false, "do not prune extra mods")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cf := &commonFlags{config: *configPath, prune: !*noPrune, verbose: *verbose}

	ui.Banner()
	ui.Blank()
	cfg, err := loadInstance(cf)
	if err != nil {
		return err
	}
	resolved, err := loadPack(cfg)
	if err != nil {
		return fmt.Errorf("couldn't load the modpack: %w", err)
	}
	manifest := resolved.Manifest
	ui.Step(fmt.Sprintf("Getting %s ready…", ui.Pink(manifest.Name)))
	ui.Detail(resolved.Coordinate)
	if resolved.Format == "mrpack" {
		ui.Detail("format: mrpack")
	}

	var jar string
	if cfg.ShouldSyncOnRun() {
		res, err := doSync(cf, cfg, resolved)
		if err != nil {
			return fmt.Errorf("refresh failed: %w", err)
		}
		printSyncSummary(res, false, manifest.Name, manifest.Version, "")
		jar = res.ServerJar
	} else {
		ui.Warn("sync_on_run = false — not refreshing pack files (local mods/config kept as-is)")
		ui.Detail("Run " + ui.Blue("./pastel refresh") + " when you want pack changes again.")
		// Still align Fabric/NeoForge launcher with pack deps (no mods download).
		if _, err := pack.EnsureLoader(cfg.Root(), manifest, ""); err != nil {
			ui.Detail("loader: " + err.Error())
		}
	}
	if jar == "" && manifest.Launch != nil {
		jar = runtime.ResolveJar(cfg.Root(), manifest.Launch.Jar)
	}
	extra := append([]string{}, cfg.ExtraJavaArgs...)
	if manifest.Launch != nil {
		extra = append(extra, manifest.Launch.ExtraArgs...)
	}

	mc := manifest.Minecraft()
	need := jre.RequireMajor(mc)
	ui.Blank()
	ui.Title("Java")
	ui.Detail(jre.FormatRequirement(mc))

	override := ""
	if cfg.Java != "" {
		override = cfg.Java
	}
	javaBin, err := jre.Ensure(cfg.Root(), need, override)
	if err != nil {
		return fmt.Errorf("Java %d is required for this pack: %w", need, err)
	}

	return runtime.Start(runtime.Options{
		Root:        cfg.Root(),
		Java:        javaBin,
		Xmx:         cfg.Xmx(),
		Manifest:    manifest,
		Jar:         jar,
		ExtraArgs:   extra,
		NoGUI:       cfg.UseNoGUI(),
		Minecraft:   mc,
		JavaMajor:   need,
		LoaderKind:  manifest.LoaderKind(),
		ModCount:    manifest.ModCount(),
		Foreground:  *foreground || *fgShort,
		AutoRestart: cfg.ShouldAutoRestart(),
	})
}

func cmdConsole(args []string) error {
	cf, _, err := parseCommon(args, false)
	if err != nil {
		return err
	}
	cfg, err := loadInstance(cf)
	if err != nil {
		return err
	}
	if _, alive, err := runtime.Running(cfg.Root()); err != nil {
		return err
	} else if !alive {
		printConsoleNotRunning()
		return softError{reason: "server is not running"}
	}
	return runtime.Attach(cfg.Root())
}

func printConsoleNotRunning() {
	ui.Banner()
	ui.Blank()
	ui.Title("Console")
	ui.Info("The server isn't running right now — nothing to attach to.")
	ui.Blank()
	ui.Step("Start it:  " + ui.Blue("./pastel run"))
	ui.Detail("Then:      " + ui.Blue("./pastel console"))
	ui.Blank()
	ui.Detail("If it just crashed, check " + ui.Blue("logs/latest.log"))
}

func cmdHome(args []string) error {
	cf, _, err := parseCommon(args, false)
	if err != nil {
		return err
	}
	ui.Banner()
	ui.Blank()
	fmt.Fprintln(ui.Out, ui.Dim("· ")+"Hi! "+ui.Brand()+" keeps this Minecraft server's mods and configs in sync.")
	ui.Info("Nothing starts automatically — pick a command when you're ready.")
	ui.Blank()

	snap, err := gatherSnapshot(cf)
	if err != nil {
		printNoConfigHome(err)
		return nil
	}
	printSnapshot(snap, false)
	ui.Blank()
	printCommandMenu(hintsFromSnapshot(snap))
	return nil
}

func printNoConfigHome(err error) {
	ui.Warn(err.Error())
	ui.Blank()
	ui.Title("Getting started")
	ui.Info("Drop " + ui.Brand() + " in an empty server folder, then install a modpack:")
	ui.Blank()
	ui.Step(ui.Blue("./pastel install aristea"))
	ui.Detail("or a Modrinth page:  " + ui.Blue("./pastel install https://modrinth.com/modpack/aristea"))
	ui.Detail("or a direct pack:    " + ui.Blue("./pastel install https://…/pack.mrpack"))
	ui.Blank()
	ui.Info("That writes " + ui.Blue("server.pastel") + " and downloads the pack for you.")
	ui.Info("Then " + ui.Blue("./pastel run") + " starts the server.")
}

func cmdStatus(args []string) error {
	cf, _, err := parseCommon(args, false)
	if err != nil {
		return err
	}
	ui.Banner()
	ui.Blank()
	snap, err := gatherSnapshot(cf)
	if err != nil {
		return err
	}
	printSnapshot(snap, true)
	return nil
}

// snapshot is a friendly view of local + desired pack state.
type snapshot struct {
	Root     string
	PackPin  string
	Memory   string
	Java     string
	State    *state.State
	Running  bool
	PID      int
	StalePID bool

	// Pinned pack (what server.pastel points at right now)
	PinName   string
	PinVer    string
	PinMC     string
	PinLoader string
	PinMods   int
	PinErr    string

	// Latest on Maven for this pack family
	LatestName      string
	LatestVer       string
	LatestMC        string
	UpdateAvailable bool
	CheckErr        string

	// From server.properties
	ServerProps mcprops.Info
}

type homeHints struct {
	noConfig        bool
	neverSync       bool
	running         bool
	updateAvailable bool
}

func gatherSnapshot(cf *commonFlags) (*snapshot, error) {
	cfg, err := loadInstance(cf)
	if err != nil {
		return nil, err
	}
	root := cfg.Root()
	s := &snapshot{
		Root:    root,
		PackPin: cfg.Pack,
		Memory:  cfg.Xmx(),
		Java:    cfg.JavaBin(),
	}
	st, err := state.Load(root)
	if err != nil {
		return nil, err
	}
	s.State = st
	pid, alive, err := runtime.Running(root)
	if err != nil {
		return nil, err
	}
	s.PID = pid
	s.Running = alive
	s.StalePID = !alive && pid != 0
	s.ServerProps = mcprops.Read(root)

	if resolved, err := loadPack(cfg); err != nil {
		s.PinErr = err.Error()
	} else {
		m := resolved.Manifest
		s.PinName = m.Name
		s.PinVer = m.Version
		s.PinMC = m.Minecraft()
		s.PinLoader = m.LoaderKind()
		s.PinMods = m.ModCount()
	}

	// Upgrade channel: Maven coordinates or modrinth:slug pins.
	baseline := ""
	if st != nil {
		baseline = st.PackVersion
	}
	if baseline == "" {
		baseline = s.PinVer
	}
	if ref, ok := pack.ParseRef(cfg.Pack); ok {
		chk, err := pack.CheckUpdate(cfg.MavenRepositories(), ref, baseline)
		if err != nil {
			s.CheckErr = err.Error()
		} else {
			s.LatestName = chk.LatestName
			s.LatestVer = chk.LatestVer
			s.LatestMC = chk.LatestMC
			s.UpdateAvailable = chk.UpdateAvailable
			if st != nil && pack.CompareVersions(st.PackVersion, chk.LatestVer) < 0 {
				s.UpdateAvailable = true
			}
		}
	} else if slug, _, ok := modrinth.ParseRef(cfg.Pack); ok {
		chk, err := modrinth.New().CheckUpdate(slug, baseline)
		if err != nil {
			s.CheckErr = err.Error()
		} else {
			s.LatestName = chk.LatestName
			s.LatestVer = chk.LatestVer
			s.LatestMC = chk.LatestMC
			s.UpdateAvailable = chk.UpdateAvailable
		}
	} else if slug, _, ok := modrinth.ParsePageURL(cfg.Pack); ok {
		chk, err := modrinth.New().CheckUpdate(slug, baseline)
		if err != nil {
			s.CheckErr = err.Error()
		} else {
			s.LatestName = chk.LatestName
			s.LatestVer = chk.LatestVer
			s.LatestMC = chk.LatestMC
			s.UpdateAvailable = chk.UpdateAvailable
		}
	}

	return s, nil
}

func printSnapshot(s *snapshot, detailed bool) {
	ui.Title("What you've got")
	if detailed {
		ui.KV("folder", s.Root)
		ui.KV("pack pin", s.PackPin)
		ui.KV("memory", s.Memory)
		ui.KV("java", s.Java)
	} else {
		ui.KV("pin", s.PackPin)
	}

	ui.Blank()
	ui.Title("Installed")
	if s.State == nil {
		ui.Info("No pack applied yet — run " + ui.Blue("./pastel refresh") + " to install your pin.")
	} else {
		mc := s.State.Minecraft
		loader := s.State.Loader
		mods := s.State.ModCount
		// Older state files may lack loader/modCount — fall back to pin pack if versions match.
		if (loader == "" || mods == 0 || mc == "") && s.PinVer == s.State.PackVersion {
			if loader == "" {
				loader = s.PinLoader
			}
			if mods == 0 {
				mods = s.PinMods
			}
			if mc == "" {
				mc = s.PinMC
			}
		}
		// Compact: name/version on one line, MC · Loader · N mods · Java on the next.
		ui.KV("pack", fmt.Sprintf("%s %s", s.State.PackName, ui.Blue("v"+s.State.PackVersion)))
		var bits []string
		if mc != "" {
			bits = append(bits, "Minecraft "+mc)
		}
		if loader != "" {
			bits = append(bits, ui.Loader(loader))
		}
		if mods > 0 {
			if mods == 1 {
				bits = append(bits, "1 mod")
			} else {
				bits = append(bits, fmt.Sprintf("%d mods", mods))
			}
		}
		if mc != "" {
			bits = append(bits, fmt.Sprintf("Java %d", jre.RequireMajor(mc)))
		}
		if len(bits) > 0 {
			line := ""
			for i, b := range bits {
				if i > 0 {
					line += ui.Dim(" · ")
				}
				line += b
			}
			fmt.Fprintln(ui.Out, "  "+strings.Repeat(" ", 12)+"  "+line)
		}
		if detailed {
			ui.KV("updated", s.State.AppliedAt.Local().Format("Jan 2, 2006 3:04 PM"))
			ui.KV("files", fmt.Sprintf("%d tracked", s.State.FileCount))
		}
	}

	ui.Blank()
	ui.Title("Server")
	if s.ServerProps.Present {
		wl := "whitelist off"
		if s.ServerProps.Whitelist {
			wl = "whitelist on"
		}
		// Compress MOTD / port / whitelist to one line; status stays separate.
		fmt.Fprintln(ui.Out, "  "+s.ServerProps.MOTD+ui.Dim(" · ")+"port "+s.ServerProps.Port+ui.Dim(" · ")+wl)
	} else {
		ui.Info("No server.properties yet — it appears after the first run")
	}
	if s.Running {
		ui.OK("Online")
	} else {
		ui.Info("Offline")
	}

	ui.Blank()
	ui.Title("Updates")
	if s.CheckErr != "" {
		ui.Warn("Couldn't check for new versions")
		ui.Detail(s.CheckErr)
	} else if s.LatestVer == "" {
		if s.PinErr != "" {
			ui.Warn("Couldn't load pin")
			ui.Detail(s.PinErr)
		} else {
			ui.Info("This pin isn't on Maven — no upgrade check")
		}
	} else if s.UpdateAvailable {
		// Line 1: what's new · Line 2: how to upgrade
		line := s.LatestName + " " + ui.Blue("v"+s.LatestVer)
		if s.LatestMC != "" {
			line += ui.Dim(" · ") + "Minecraft " + s.LatestMC
			line += ui.Dim(" · ") + fmt.Sprintf("Java %d", jre.RequireMajor(s.LatestMC))
		}
		ui.Warn("New version: " + line)
		ui.Detail("Run " + ui.Blue("./pastel update") + " to upgrade")
	} else {
		// Line 1: latest · Line 2 only if we have MC detail worth showing — fold into one when possible
		line := "On the latest pack (" + ui.Blue("v"+s.LatestVer) + ")"
		if s.LatestMC != "" {
			line += ui.Dim(" · ") + "Minecraft " + s.LatestMC + ui.Dim(" · ") + fmt.Sprintf("Java %d", jre.RequireMajor(s.LatestMC))
		}
		ui.OK(line)
	}

	if detailed && s.PinVer != "" {
		ui.Blank()
		ui.Title("Your pin")
		if s.PinErr != "" {
			ui.Warn(s.PinErr)
		} else {
			ui.KV("pack", pack.FormatPackLine(s.PinName, s.PinVer, s.PinMC))
		}
	}
}

func hintsFromSnapshot(s *snapshot) homeHints {
	return homeHints{
		neverSync:       s.State == nil,
		running:         s.Running,
		updateAvailable: s.UpdateAvailable,
	}
}

func printCommandMenu(h homeHints) {
	ui.Title("Suggested next step")
	switch {
	case h.noConfig:
		ui.Step("Add a server.pastel file next to this program, then run ./pastel again.")
	case h.neverSync:
		ui.Step("Files not downloaded yet — " + ui.Blue("./pastel refresh") + " then " + ui.Blue("./pastel run") + ".")
	case h.updateAvailable:
		ui.Step("A newer pack is out — run " + ui.Blue("./pastel update") + " and pick the version you want.")
	case h.running:
		ui.Step("Server is up — " + ui.Blue("./pastel console") + " for logs/commands, or " + ui.Blue("./pastel stop") + " to shut down.")
	default:
		ui.Step("Ready when you are: " + ui.Blue("./pastel run") + " starts the server in the background.")
	}
	ui.Blank()
	ui.Info(ui.Brand() + " can update packs, keep mods in sync, and run the server for you.")
	ui.Detail("See every command:  " + ui.Blue("./pastel help"))
}

func cmdStop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	force := fs.Bool("force", false, "skip graceful stop; SIGTERM/SIGKILL immediately")
	pid := fs.Int("pid", 0, "stop this process id (after a deleted server folder)")
	orphans := fs.Bool("orphans", false, "stop Minecraft servers whose folder was deleted while running")
	configPath := fs.String("config", "", "path to server.pastel")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ui.Banner()
	ui.Blank()

	// -orphans / -pid work without a pack config (folder may be empty or new).
	if *orphans {
		return runtime.StopWith(".", runtime.StopOptions{Orphans: true})
	}
	if *pid > 0 {
		root := "."
		if *configPath != "" {
			if cfg, err := config.Load(*configPath); err == nil {
				root = cfg.Root()
			}
		} else if cwd, err := os.Getwd(); err == nil {
			root = cwd
		}
		return runtime.StopWith(root, runtime.StopOptions{PID: *pid})
	}

	cf := &commonFlags{config: *configPath}
	cfg, err := loadInstance(cf)
	if err != nil {
		// No server.pastel — still try orphan recovery for this folder + global orphans.
		cwd, _ := os.Getwd()
		ui.Detail("no server.pastel here — checking for processes in " + cwd)
		if err := runtime.StopWith(cwd, runtime.StopOptions{Force: *force}); err != nil {
			return err
		}
		return nil
	}
	return runtime.StopWith(cfg.Root(), runtime.StopOptions{Force: *force})
}

func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	to := fs.String("to", "", "pack version to install (skips the picker if set)")
	yes := fs.Bool("yes", false, "skip the confirmation prompt (for scripts)")
	dryRun := fs.Bool("dry-run", false, "show the plan without applying")
	verbose := fs.Bool("v", false, "verbose")
	configPath := fs.String("config", "", "path to server.pastel")
	noPrune := fs.Bool("no-prune", false, "do not prune extra mods")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cf := &commonFlags{config: *configPath, prune: !*noPrune, dryRun: *dryRun, verbose: *verbose}
	cfg, err := loadInstance(cf)
	if err != nil {
		return err
	}
	if err := requireServerStopped(cfg.Root()); err != nil {
		return err
	}

	// Route by pin kind
	if slug, _, ok := modrinth.ParseRef(cfg.Pack); ok {
		return cmdUpdateModrinth(cf, cfg, slug, *to, *yes, *dryRun)
	}
	if slug, _, ok := modrinth.ParsePageURL(cfg.Pack); ok {
		return cmdUpdateModrinth(cf, cfg, slug, *to, *yes, *dryRun)
	}
	if ref, ok := pack.ParseRef(cfg.Pack); ok {
		return cmdUpdateMaven(cf, cfg, ref, *to, *yes, *dryRun)
	}
	return fmt.Errorf("pack pin %q isn't updatable that way — use ./pastel install for a new pack, or pin modrinth:… / Maven group:artifact:version", cfg.Pack)
}

func cmdUpdateMaven(cf *commonFlags, cfg *config.Config, ref pack.Ref, to string, yes, dryRun bool) error {
	ui.Banner()
	ui.Blank()
	fmt.Fprintln(ui.Out, ui.Dim("· ")+"Looking up pack versions on Maven…")
	ui.Blank()

	st, err := state.Load(cfg.Root())
	if err != nil {
		return err
	}
	baseline := ""
	baselineMC := ""
	if st != nil {
		baseline = st.PackVersion
		baselineMC = st.Minecraft
	}

	repos := cfg.MavenRepositories()
	cl := maven.NewClient(repos...)
	versions, release, err := cl.ListVersions(ref.Group, ref.Artifact)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		return fmt.Errorf("no versions published for %s:%s", ref.Group, ref.Artifact)
	}

	cache := filepath.Join(cfg.Root(), ".pastel", "cache", "packs")
	infos := make([]packVersionInfo, 0, len(versions))
	for _, v := range versions {
		coord := ref.Coordinate(v)
		resolved, err := pack.Resolve(pack.ResolveSpec{
			Raw: coord, Repositories: repos, CacheDir: cache, Side: pack.SideServer,
		})
		if err != nil {
			infos = append(infos, packVersionInfo{Version: v})
			continue
		}
		m := resolved.Manifest
		infos = append(infos, packVersionInfo{
			Version:   m.Version,
			Minecraft: m.Minecraft(),
			Name:      m.Name,
			Resolved:  resolved,
			Pin:       coord,
		})
	}

	targetVer := strings.TrimSpace(to)
	if targetVer == "" {
		picked, err := pickPackVersion(infos, baseline, release)
		if err != nil {
			if err.Error() == "cancelled" {
				ui.Info("Cancelled — no changes made.")
				return nil
			}
			return err
		}
		targetVer = picked
	}

	var target *pack.Resolved
	var targetPin string
	for _, info := range infos {
		if info.Version == targetVer {
			target = info.Resolved
			targetPin = info.Pin
			break
		}
	}
	if target == nil {
		coord := ref.Coordinate(targetVer)
		resolved, err := pack.Resolve(pack.ResolveSpec{
			Raw: coord, Repositories: repos, CacheDir: cache, Side: pack.SideServer,
		})
		if err != nil {
			return fmt.Errorf("couldn't load pack %s: %w", coord, err)
		}
		target = resolved
		targetVer = resolved.Manifest.Version
		targetPin = coord
	}
	if targetPin == "" {
		targetPin = ref.Coordinate(targetVer)
	}
	return applyPackUpgrade(cf, cfg, st, baseline, baselineMC, target, targetVer, targetPin, yes, dryRun)
}

func cmdUpdateModrinth(cf *commonFlags, cfg *config.Config, slug, to string, yes, dryRun bool) error {
	ui.Banner()
	ui.Blank()
	fmt.Fprintln(ui.Out, ui.Dim("· ")+"Looking up pack versions on Modrinth…")
	ui.Blank()

	st, err := state.Load(cfg.Root())
	if err != nil {
		return err
	}
	baseline := ""
	baselineMC := ""
	if st != nil {
		baseline = st.PackVersion
		baselineMC = st.Minecraft
	}

	cl := modrinth.New()
	proj, versions, err := cl.ListMrpackVersions(slug)
	if err != nil {
		return err
	}
	// Cap list length for the picker (newest first from API)
	const maxList = 40
	if len(versions) > maxList {
		versions = versions[:maxList]
	}

	release := ""
	for _, v := range versions {
		if strings.EqualFold(v.VersionType, "release") {
			release = v.VersionNumber
			break
		}
	}
	if release == "" && len(versions) > 0 {
		release = versions[0].VersionNumber
	}

	// Picker uses API metadata only (no downloading every .mrpack).
	slugID := firstNonEmpty(proj.Slug, proj.ID, slug)
	name := firstNonEmpty(proj.Title, proj.Slug)
	infos := make([]packVersionInfo, 0, len(versions))
	for _, v := range versions {
		mc := ""
		if len(v.GameVersions) > 0 {
			mc = v.GameVersions[len(v.GameVersions)-1]
		}
		infos = append(infos, packVersionInfo{
			Version:   v.VersionNumber,
			Minecraft: mc,
			Name:      name,
			Pin:       modrinth.Pin(slugID, v.VersionNumber),
			Tag:       v.VersionType,
		})
	}

	targetVer := strings.TrimSpace(to)
	if targetVer == "" {
		picked, err := pickPackVersion(infos, baseline, release)
		if err != nil {
			if err.Error() == "cancelled" {
				ui.Info("Cancelled — no changes made.")
				return nil
			}
			return err
		}
		targetVer = picked
	}

	targetPin := modrinth.Pin(slugID, targetVer)
	for _, info := range infos {
		if info.Version == targetVer {
			targetPin = info.Pin
			break
		}
	}

	cache := filepath.Join(cfg.Root(), ".pastel", "cache", "packs")
	target, err := pack.Resolve(pack.ResolveSpec{
		Raw: targetPin, CacheDir: cache, Side: pack.SideServer,
	})
	if err != nil {
		return fmt.Errorf("couldn't load pack %s: %w", targetPin, err)
	}
	if target.Manifest.Version != "" {
		targetVer = target.Manifest.Version
	}
	return applyPackUpgrade(cf, cfg, st, baseline, baselineMC, target, targetVer, targetPin, yes, dryRun)
}

func applyPackUpgrade(cf *commonFlags, cfg *config.Config, st *state.State, baseline, baselineMC string, target *pack.Resolved, targetVer, targetPin string, yes, dryRun bool) error {
	if target == nil || target.Manifest == nil {
		return fmt.Errorf("internal: missing target pack")
	}
	targetManifest := target.Manifest

	if baseline != "" && baseline == targetVer {
		ui.OK(fmt.Sprintf("Already on v%s.", baseline))
		ui.Info("To re-download files for this version, run " + ui.Blue("./pastel refresh") + ".")
		return nil
	}

	ui.Blank()
	ui.Title("Confirm upgrade")
	from := "nothing installed"
	if baseline != "" {
		from = pack.FormatPackLine("installed", baseline, baselineMC)
		if st != nil && st.PackName != "" {
			from = pack.FormatPackLine(st.PackName, baseline, baselineMC)
		}
	}
	toLine := pack.FormatPackLine(targetManifest.Name, targetVer, targetManifest.Minecraft())
	ui.KV("from", from)
	ui.KV("to", toLine)
	ui.Detail(targetPin)
	if baseline != "" && pack.CompareVersions(baseline, targetVer) > 0 {
		ui.Warn("This is a downgrade.")
	}

	if dryRun {
		ui.Blank()
		ui.Warn("Dry run — nothing was changed.")
		return nil
	}

	if !yes {
		ok, err := confirmYesNo("Proceed with this upgrade? " + ui.Pink("[y/N]") + " ")
		if err != nil {
			return err
		}
		if !ok {
			ui.Info("Cancelled — no changes made.")
			return nil
		}
	}

	if err := cfg.SetPack(targetPin); err != nil {
		return fmt.Errorf("couldn't update server.pastel pin: %w", err)
	}
	ui.OK("Updated pin → " + ui.Blue(targetPin))
	ui.Blank()
	ui.Step(fmt.Sprintf("Downloading %s…", ui.Pink(targetManifest.Name)+" "+ui.Blue("v"+targetVer)))

	target.Coordinate = targetPin
	// Reload cfg so path/pack match disk
	cfg2, err := config.Load(cfg.Path())
	if err != nil {
		return err
	}
	res, err := doSync(cf, cfg2, target)
	if err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}
	next := "Start the server with " + ui.Blue("./pastel run") + ", then " + ui.Blue("./pastel console") + " for the live log."
	printSyncSummary(res, false, targetManifest.Name, targetVer, next)
	return nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

type packVersionInfo struct {
	Version   string
	Minecraft string
	Name      string
	Resolved  *pack.Resolved
	Pin       string // full pin written to server.pastel
	Tag       string // e.g. release / beta (Modrinth)
}

func pickPackVersion(infos []packVersionInfo, installed, release string) (string, error) {
	ui.Title("Pick a pack version")
	if installed != "" {
		ui.Detail("Currently installed: v" + installed)
	}
	ui.Blank()

	for i, info := range infos {
		label := fmt.Sprintf("%s)  v%s", ui.Blue(fmt.Sprintf("%d", i+1)), info.Version)
		if info.Minecraft != "" {
			label += "  ·  Minecraft " + info.Minecraft
			label += "  ·  Java " + fmt.Sprint(jre.RequireMajor(info.Minecraft))
		}
		tags := []string{}
		if info.Version == release {
			tags = append(tags, "latest")
		}
		if info.Tag != "" && !strings.EqualFold(info.Tag, "release") {
			tags = append(tags, info.Tag)
		}
		if installed != "" && info.Version == installed {
			tags = append(tags, "installed")
		}
		if len(tags) > 0 {
			label += "  " + ui.Dim("("+strings.Join(tags, ", ")+")")
		}
		fmt.Fprintln(ui.Out, "  "+label)
	}
	ui.Blank()

	if !stdinIsTTY() {
		return "", fmt.Errorf("no TTY for version picker — pass -to VERSION (and -yes to skip confirm)")
	}

	fmt.Fprint(ui.Out, ui.Blue("Version number or id")+ui.Dim(" (q cancels): "))
	line, err := readLine()
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" || strings.EqualFold(line, "q") || strings.EqualFold(line, "quit") || strings.EqualFold(line, "cancel") {
		return "", fmt.Errorf("cancelled")
	}
	// numeric index
	if n, err := strconv.Atoi(line); err == nil {
		if n < 1 || n > len(infos) {
			return "", fmt.Errorf("pick a number between 1 and %d", len(infos))
		}
		return infos[n-1].Version, nil
	}
	// bare version or v-prefixed
	line = strings.TrimPrefix(line, "v")
	for _, info := range infos {
		if info.Version == line {
			return info.Version, nil
		}
	}
	return "", fmt.Errorf("unknown version %q", line)
}

func confirmYesNo(prompt string) (bool, error) {
	if !stdinIsTTY() {
		return false, fmt.Errorf("no TTY for confirmation — pass -yes to confirm non-interactively")
	}
	fmt.Fprint(ui.Out, ui.Blue(prompt))
	line, err := readLine()
	if err != nil {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

func readLine() (string, error) {
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("no input")
	}
	return sc.Text(), nil
}

func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
