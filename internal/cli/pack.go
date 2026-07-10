package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/iamkaf/pastel/internal/author"
	"github.com/iamkaf/pastel/internal/ui"
)

func cmdPack(args []string) error {
	if len(args) == 0 {
		printPackHelp()
		return nil
	}
	switch args[0] {
	case "build":
		return friendly(cmdPackBuild(args[1:]))
	case "publish":
		return friendly(cmdPackPublish(args[1:]))
	case "help", "-h", "--help":
		printPackHelp()
		return nil
	default:
		return friendly(fmt.Errorf("unknown pack command %q — try: pastel pack help", args[0]))
	}
}

func printPackHelp() {
	ui.Banner()
	ui.Blank()
	fmt.Fprintln(os.Stderr, ui.Pink("Pack authoring (you, not the friend)"))
	fmt.Fprintln(os.Stderr, "  "+ui.Blue("pastel pack build")+"     Build a .mrpack from index + server config overrides")
	fmt.Fprintln(os.Stderr, "  "+ui.Blue("pastel pack publish")+"   Upload the .mrpack to Kaf Maven")
	ui.Blank()
	fmt.Fprintln(os.Stderr, ui.Dim("Example:"))
	fmt.Fprintln(os.Stderr, ui.Dim("  pastel pack build -server ./staging/forever-world-1.1.0-server \\"))
	fmt.Fprintln(os.Stderr, ui.Dim("    -mrpack ./staging/forever-world-1.1.0-server/modrinth.index.json \\"))
	fmt.Fprintln(os.Stderr, ui.Dim("    -name \"FOREVER WORLD\" -slug forever-world -version 1.1.0 \\"))
	fmt.Fprintln(os.Stderr, ui.Dim("    -out dist/forever-world/1.1.0"))
	fmt.Fprintln(os.Stderr, ui.Dim("  pastel pack publish -dir dist/forever-world/1.1.0"))
}

func cmdPackBuild(args []string) error {
	fs := flag.NewFlagSet("pack build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	server := fs.String("server", "", "server directory (config/ → server-overrides/)")
	mrpack := fs.String("mrpack", "", "modrinth.index.json or pack directory (required)")
	name := fs.String("name", "", "display name")
	slug := fs.String("slug", "", "Maven artifact id (e.g. forever-world)")
	version := fs.String("version", "", "pack version (immutable)")
	out := fs.String("out", "", "output directory")
	group := fs.String("group", "com.iamkaf.modpacks", "Maven groupId")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ui.Banner()
	ui.Blank()
	ui.Title("Building .mrpack")
	res, err := author.Build(author.BuildOptions{
		ServerDir: *server,
		Mrpack:    *mrpack,
		Name:      *name,
		Slug:      *slug,
		Version:   *version,
		OutDir:    *out,
		Group:     *group,
		Side:      "server",
	})
	if err != nil {
		return err
	}
	ui.OK(fmt.Sprintf("%s v%s", res.Index.Name, res.Index.VersionID))
	ui.KV("files", fmt.Sprintf("%d in index", res.FileCount))
	ui.KV("out", res.OutDir)
	ui.Detail(filepath.Base(res.MrpackPath))
	ui.Detail(filepath.Base(res.POM))
	ui.Blank()
	ui.Info("Next: pastel pack publish -dir " + res.OutDir)
	return nil
}

func cmdPackPublish(args []string) error {
	fs := flag.NewFlagSet("pack publish", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", "", "build output directory")
	dry := fs.Bool("dry-run", false, "print uploads only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ui.Banner()
	ui.Blank()
	ui.Title("Publishing to Kaf Maven")
	res, err := author.Publish(author.PublishOptions{
		Dir:    *dir,
		DryRun: *dry,
	})
	if err != nil {
		return err
	}
	for _, u := range res.Uploaded {
		ui.OK(u)
	}
	ui.Blank()
	if *dry {
		ui.Info("Dry run only — nothing uploaded.")
	} else {
		ui.OK("Published. Friends can pin:")
		if res.Pin != "" {
			ui.Detail(`pack = "` + res.Pin + `"`)
		} else {
			ui.Detail(`pack = "com.iamkaf.modpacks:…:…"`)
		}
	}
	return nil
}
