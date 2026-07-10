// Package runtime supervises the Minecraft dedicated server process.
package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/iamkaf/pastel/internal/pack"
	"github.com/iamkaf/pastel/internal/state"
	"github.com/iamkaf/pastel/internal/ui"
)

// Options for starting the server.
type Options struct {
	Root string
	Java string
	Xmx  string
	// Manifest drives multi-loader launch (jar vs @args files).
	Manifest *pack.Manifest
	// Jar is optional legacy absolute path; ignored when Manifest.Launch is set.
	Jar       string
	ExtraArgs []string
	NoGUI     bool
	Minecraft string
	JavaMajor int
	// LoaderKind is e.g. "Fabric" (shown in theme color).
	LoaderKind string
	// ModCount is how many mod jars the pack installs.
	ModCount int
	// Foreground keeps the old attach-to-this-terminal behavior.
	Foreground bool
}

// Running reports whether this server's Minecraft process is alive.
// Prefers the pid file; if missing/stale, scans for orphan Java processes for this root.
func Running(root string) (pid int, alive bool, err error) {
	if pid, ok := readAlivePID(state.PIDPath(root)); ok {
		return pid, true, nil
	}
	// Recover from lost/corrupt pid files (orphans after failed stop or pid -1 bug).
	orphans := findServerProcesses(root)
	if len(orphans) > 0 {
		return orphans[0], true, nil
	}
	return 0, false, nil
}

func holdRunning(root string) (pid int, alive bool) {
	pid, ok := readAlivePID(state.HoldPIDPath(root))
	return pid, ok
}

func readAlivePID(path string) (pid int, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if !processAlive(pid) {
		return pid, false
	}
	return pid, true
}

// StopOptions control how Stop finds and kills processes.
type StopOptions struct {
	// Force skips the graceful "stop" console command and SIGTERMs immediately.
	Force bool
	// PID kills only this process (still cleans tracking files for root).
	PID int
	// Orphans kills Minecraft servers whose folder was deleted under them.
	Orphans bool
}

// Stop gracefully stops the server and any orphans for this root.
func Stop(root string) error {
	return StopWith(root, StopOptions{})
}

// StopWith stops using explicit options (force / pid / orphans).
func StopWith(root string, opt StopOptions) error {
	if opt.PID > 0 {
		ui.Step(fmt.Sprintf("Stopping process %d…", opt.PID))
		if err := KillPID(opt.PID); err != nil {
			return err
		}
		// If it was ours, clear tracking.
		cleanupServerFiles(root)
		ui.OK(fmt.Sprintf("Process %d stopped.", opt.PID))
		return nil
	}

	if opt.Orphans {
		return stopOrphans()
	}

	infos := findServerProcessInfos(root)
	pids := serverPIDs(root)
	// Merge infos into pids if discovery found more
	seen := map[int]bool{}
	for _, p := range pids {
		seen[p] = true
	}
	for _, info := range infos {
		if !seen[info.PID] {
			pids = append(pids, info.PID)
			seen[info.PID] = true
		}
	}

	if len(pids) == 0 {
		_ = os.Remove(state.PIDPath(root))
		stopHold(root)
		// Help after "I deleted the folder while the server was running"
		orphans := FindOrphanMinecraftServers()
		if len(orphans) == 0 {
			ui.Info("The server is not running.")
			return nil
		}
		ui.Warn("Nothing is tracked for this folder, but other Minecraft server process(es) are still running:")
		for _, o := range orphans {
			cwd := o.Cwd
			if cwd == "" {
				cwd = "(unknown folder)"
			}
			ui.Detail(fmt.Sprintf("pid %d  ·  %s", o.PID, cwd))
		}
		ui.Blank()
		ui.Step("Stop a specific one:  " + ui.Blue(fmt.Sprintf("./pastel stop -pid %d", orphans[0].PID)))
		if len(orphans) > 1 {
			ui.Step("Or stop all deleted-folder servers:  " + ui.Blue("./pastel stop -orphans"))
		} else {
			ui.Detail("Or:  " + ui.Blue("./pastel stop -orphans"))
		}
		return nil
	}

	ui.Step("Stopping the server…")
	if !opt.Force {
		// Prefer a clean Minecraft stop via console when possible.
		// Must not hang if FIFO has no reader (deleted folder / crashed server).
		if err := SendCommand(root, "stop"); err != nil {
			ui.Detail("no live console — signalling the process directly")
		} else {
			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				if len(serverPIDs(root)) == 0 && len(findServerProcesses(root)) == 0 {
					cleanupServerFiles(root)
					ui.OK("Server stopped. See you next time!")
					return nil
				}
				time.Sleep(200 * time.Millisecond)
			}
			ui.Warn("Still shutting down… asking a bit harder.")
		}
	}

	for _, pid := range serverPIDs(root) {
		_ = signalPID(pid, syscall.SIGTERM)
	}
	for _, pid := range findServerProcesses(root) {
		_ = signalPID(pid, syscall.SIGTERM)
	}
	time.Sleep(3 * time.Second)
	left := serverPIDs(root)
	for _, pid := range findServerProcesses(root) {
		left = append(left, pid)
	}
	// unique
	seen = map[int]bool{}
	var uniq []int
	for _, p := range left {
		if !seen[p] && processAlive(p) {
			seen[p] = true
			uniq = append(uniq, p)
		}
	}
	if len(uniq) > 0 {
		ui.Warn("Forcing shutdown.")
		for _, pid := range uniq {
			_ = signalPID(pid, syscall.SIGKILL)
		}
	}
	cleanupServerFiles(root)
	ui.OK("Server stopped.")
	return nil
}

func stopOrphans() error {
	orphans := FindOrphanMinecraftServers()
	if len(orphans) == 0 {
		ui.Info("No orphan Minecraft servers found (deleted-folder processes).")
		return nil
	}
	ui.Step(fmt.Sprintf("Stopping %d orphan server process(es)…", len(orphans)))
	for _, o := range orphans {
		ui.Detail(fmt.Sprintf("pid %d  ·  %s", o.PID, o.Cwd))
		if err := KillPID(o.PID); err != nil {
			ui.Warn(err.Error())
			continue
		}
		ui.OK(fmt.Sprintf("Stopped %d", o.PID))
	}
	return nil
}

func serverPIDs(root string) []int {
	var pids []int
	seen := map[int]bool{}
	if pid, ok := readAlivePID(state.PIDPath(root)); ok {
		pids = append(pids, pid)
		seen[pid] = true
	}
	for _, pid := range findServerProcesses(root) {
		if !seen[pid] {
			pids = append(pids, pid)
			seen[pid] = true
		}
	}
	return pids
}

func cleanupServerFiles(root string) {
	_ = os.Remove(state.PIDPath(root))
	stopHold(root)
}

func stopHold(root string) {
	if pid, ok := holdRunning(root); ok {
		_ = signalPID(pid, syscall.SIGKILL)
	}
	_ = os.Remove(state.HoldPIDPath(root))
	_ = os.Remove(state.ConsoleInPath(root))
}

// Start launches the server in the background (default) or foreground.
func Start(opt Options) error {
	if _, alive, _ := Running(opt.Root); alive {
		return fmt.Errorf("the server is already running — try: ./pastel console  or  ./pastel stop")
	}
	if orphans := findServerProcesses(opt.Root); len(orphans) > 0 {
		return fmt.Errorf("the server is already running — try: ./pastel console  or  ./pastel stop")
	}

	if err := EnsureEULA(opt.Root); err != nil {
		return fmt.Errorf("couldn't accept the Minecraft EULA: %w", err)
	}
	if err := os.MkdirAll(state.Dir(opt.Root), 0o755); err != nil {
		return err
	}

	// Ensure Forge/NeoForge user_jvm_args.txt exists when referenced.
	if opt.Manifest != nil && opt.Manifest.Launch != nil && opt.Manifest.Launch.JVMArgsFile != "" {
		_ = ensureUserJVMArgs(opt.Root, opt.Manifest.Launch.JVMArgsFile, opt.Xmx)
	}

	java := opt.Java
	if java == "" {
		java = "java"
	}
	xmx := opt.Xmx
	if xmx == "" {
		xmx = "4G"
	}

	var args []string
	var err error
	if opt.Manifest != nil {
		args, err = opt.Manifest.BuildJavaArgs(opt.Root, xmx)
		if err != nil {
			// Fall back to legacy jar path
			if opt.Jar != "" {
				args = []string{"-Xmx" + xmx}
				args = append(args, opt.ExtraArgs...)
				args = append(args, "-jar", opt.Jar)
				if opt.NoGUI {
					args = append(args, "nogui")
				}
				err = nil
			}
		} else if len(opt.ExtraArgs) > 0 {
			// Insert extra JVM args after -Xmx
			args = append([]string{args[0]}, append(opt.ExtraArgs, args[1:]...)...)
		}
	} else if opt.Jar != "" {
		args = []string{"-Xmx" + xmx}
		args = append(args, opt.ExtraArgs...)
		args = append(args, "-jar", opt.Jar)
		if opt.NoGUI {
			args = append(args, "nogui")
		}
	} else {
		return fmt.Errorf("couldn't find the server program to launch")
	}
	if err != nil {
		return err
	}

	ui.Blank()
	ui.Title("Starting your Minecraft server")
	var bits []string
	if opt.Minecraft != "" {
		bits = append(bits, "Minecraft "+opt.Minecraft)
	}
	if opt.LoaderKind != "" {
		bits = append(bits, ui.Loader(opt.LoaderKind))
	}
	bits = append(bits, xmx)
	if opt.ModCount > 0 {
		if opt.ModCount == 1 {
			bits = append(bits, "1 mod")
		} else {
			bits = append(bits, fmt.Sprintf("%d mods", opt.ModCount))
		}
	}
	line := ""
	for i, b := range bits {
		if i > 0 {
			line += ui.Dim(" · ")
		}
		line += b
	}
	fmt.Fprintln(ui.Out, "  "+line)
	if opt.JavaMajor > 0 {
		ui.Detail(fmt.Sprintf("Java %d", opt.JavaMajor))
	}
	ui.Detail("By running this server you agree to Mojang's EULA")
	ui.Detail("https://aka.ms/MinecraftEULA")

	if opt.Foreground {
		return startForeground(opt, java, args)
	}
	return startBackground(opt, java, args)
}

func ensureUserJVMArgs(root, rel, xmx string) error {
	p := filepath.Join(root, filepath.FromSlash(rel))
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return nil
	}
	if xmx == "" {
		xmx = "4G"
	}
	body := fmt.Sprintf(`# Generated by Pastel — friend memory setting is applied as -Xmx on the command line too.
-Xmx%s
`, xmx)
	return os.WriteFile(p, []byte(body), 0o644)
}

func startForeground(opt Options, java string, args []string) error {
	ui.Info("Foreground mode — press Ctrl+C or type stop to shut down.")
	ui.Blank()
	cmd := exec.Command(java, args...)
	cmd.Dir = opt.Root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("couldn't start Java (%s): %w", java, err)
	}
	_ = os.WriteFile(state.PIDPath(opt.Root), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644)
	err := cmd.Wait()
	_ = os.Remove(state.PIDPath(opt.Root))
	ui.Blank()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			ui.Warn(fmt.Sprintf("Server exited (code %d).", exit.ExitCode()))
			return nil
		}
		return err
	}
	ui.OK("Server shut down cleanly.")
	return nil
}

// ExplainedError means the user already saw a plain-language explanation;
// the CLI should not print a second scary "Something went wrong" box.
type ExplainedError struct {
	Msg string
}

func (e *ExplainedError) Error() string {
	if e == nil || e.Msg == "" {
		return "server failed to start"
	}
	return e.Msg
}

func earlyExitError(root string, captureLog, latestLog string) error {
	logText := readLogTail(latestLog, 64*1024)
	if logText == "" {
		logText = readLogTail(captureLog, 64*1024)
	}
	summary := summarizeCrash(logText)

	// Same visual weight as “Your server is running” — impossible to miss.
	ui.BigFail("The server couldn't start")
	ui.Info("Minecraft quit before it was ready to play.")
	ui.Blank()

	if summary.Headline != "" {
		ui.Warn(summary.Headline)
	}
	for _, line := range summary.Details {
		ui.Detail(line)
	}
	if len(summary.Mods) > 0 {
		ui.Blank()
		ui.Title("Mods that look involved")
		for _, m := range summary.Mods {
			fmt.Fprintln(ui.Out, "  "+ui.Pink("· ")+m)
		}
	}

	ui.Blank()
	ui.Title("What you can try")
	ui.Step("Remove the problem jar(s) from the " + ui.Blue("mods/") + " folder")
	ui.Step("Or reinstall the pack cleanly:  " + ui.Blue("./pastel install … -yes"))
	ui.Detail("While debugging: set " + ui.Blue("sync_on_run = false") + " in server.pastel so run doesn't put jars back")
	ui.Blank()
	ui.Detail("Full technical log: " + ui.Blue("logs/latest.log"))
	return &ExplainedError{Msg: "server couldn't start"}
}

// crashSummary is a friend-readable interpretation of a Minecraft crash log.
type crashSummary struct {
	Headline string
	Details  []string
	Mods     []string
}

func summarizeCrash(logText string) crashSummary {
	if logText == "" {
		return crashSummary{
			Headline: "We couldn't read a useful error from the log.",
			Details:  []string{"Open logs/latest.log if you want the full details."},
		}
	}
	low := strings.ToLower(logText)
	mods := extractModNames(logText)

	// Client-only / wrong-side mods (very common for friends)
	if strings.Contains(low, "worldrenderevents") ||
		strings.Contains(low, "fabric/api/client") ||
		strings.Contains(low, "is marked as \"client\"") ||
		strings.Contains(low, "invalid player data") && strings.Contains(low, "client") {
		s := crashSummary{
			Headline: "A client-only mod is installed on this dedicated server.",
			Details: []string{
				"Some mods are for the game you play on your computer, not for a server.",
				"They need to be removed from the server's mods folder.",
			},
			Mods: mods,
		}
		return s
	}

	if strings.Contains(low, "could not execute entrypoint") || strings.Contains(low, "failed to start the minecraft server") {
		s := crashSummary{
			Headline: "A mod failed while Minecraft was starting up.",
			Details: []string{
				"Usually a bad, outdated, or client-only jar in mods/.",
			},
			Mods: mods,
		}
		if len(mods) == 0 {
			s.Details = append(s.Details, "Check logs/latest.log for the mod name if you're unsure.")
		}
		return s
	}

	if strings.Contains(low, "mixin") && (strings.Contains(low, "error") || strings.Contains(low, "fail")) {
		return crashSummary{
			Headline: "Mods are conflicting with each other (or with this Minecraft version).",
			Details: []string{
				"Try removing recently added jars from mods/, then run again.",
			},
			Mods: mods,
		}
	}

	if strings.Contains(low, "out of memory") || strings.Contains(low, "outofmemory") {
		return crashSummary{
			Headline: "The server ran out of memory.",
			Details: []string{
				"Increase memory in server.pastel, for example: memory = \"6G\"",
			},
		}
	}

	if strings.Contains(low, "unsupported class file") || strings.Contains(low, "java.lang.class") && strings.Contains(low, "version") {
		return crashSummary{
			Headline: "This pack needs a different Java version.",
			Details: []string{
				"Pastel usually picks Java automatically — try ./pastel run again after a refresh.",
			},
		}
	}

	// Generic fallback — still no stack dump
	s := crashSummary{
		Headline: "Something in the pack or mods folder stopped the server.",
		Details: []string{
			"This is often a bad jar under mods/, or a pack version mismatch.",
		},
		Mods: mods,
	}
	return s
}

// extractModNames pulls likely mod ids / jar names from Fabric-style crash text.
func extractModNames(logText string) []string {
	// provided by 'modid' at '...'
	provided := regexp.MustCompile(`(?i)provided by '([^']+)'`)
	// ~[some-mod-1.2.3.jar:?]
	jarRef := regexp.MustCompile(`~\[([a-zA-Z0-9._+-]+\.jar)`)
	// mods/foo.jar in paths
	modsPath := regexp.MustCompile(`mods[/\\]([a-zA-Z0-9._+-]+\.jar)`)

	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		// skip loader noise
		low := strings.ToLower(s)
		if strings.Contains(low, "fabric-loader") || strings.Contains(low, "java.base") ||
			strings.Contains(low, "server-intermediary") || low == "minecraft" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
		if len(out) >= 6 {
			return
		}
	}

	for _, m := range provided.FindAllStringSubmatch(logText, 8) {
		if len(m) > 1 {
			add(m[1])
		}
	}
	for _, m := range jarRef.FindAllStringSubmatch(logText, 12) {
		if len(m) > 1 {
			add(m[1])
		}
	}
	for _, m := range modsPath.FindAllStringSubmatch(logText, 8) {
		if len(m) > 1 {
			add(m[1])
		}
	}
	return out
}

func readLogTail(path string, maxBytes int) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	return string(data)
}

func padRunCmd(s string) string {
	const w = 18
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

// ResolveJar returns an absolute jar path.
func ResolveJar(root, jar string) string {
	if jar == "" {
		return ""
	}
	if filepath.IsAbs(jar) {
		return jar
	}
	return filepath.Join(root, jar)
}

// EnsureEULA writes eula.txt with eula=true so the server can start without a manual edit.
// https://aka.ms/MinecraftEULA
func EnsureEULA(root string) error {
	path := filepath.Join(root, "eula.txt")
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			if strings.EqualFold(line, "eula=true") {
				return nil
			}
		}
	}
	body := `# By changing the setting below to TRUE you are indicating your agreement to our EULA (https://aka.ms/MinecraftEULA).
# Accepted automatically by Pastel when starting the server.
eula=true
`
	return os.WriteFile(path, []byte(body), 0o644)
}
