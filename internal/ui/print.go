package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Out is the default writer for human messages (stdout).
var Out io.Writer = os.Stdout

// Err is used for errors and help (stderr).
var Err io.Writer = os.Stderr

// logoLines is the PASTEL block wordmark (half-block braille-style glyphs).
var logoLines = []string{
	`▗▄▄▖  ▗▄▖  ▗▄▄▖▗▄▄▄▖▗▄▄▄▖▗▖   `,
	`▐▌ ▐▌▐▌ ▐▌▐▌     █  ▐▌   ▐▌   `,
	`▐▛▀▘ ▐▛▀▜▌ ▝▀▚▖  █  ▐▛▀▀▘▐▌   `,
	`▐▌   ▐▌ ▐▌▗▄▄▞▘  █  ▐▙▄▄▖▐▙▄▄▖`,
}

// Banner prints the PASTEL ASCII wordmark and tagline.
func Banner() {
	for i, line := range logoLines {
		if i%2 == 0 {
			fmt.Fprintln(Out, Pink(line))
		} else {
			fmt.Fprintln(Out, Blue(line))
		}
	}
	fmt.Fprintln(Out, Dim("  your Minecraft server helper"))
}

// Title prints a section header.
func Title(text string) {
	fmt.Fprintln(Out, Pink("◆ ")+Bold(Blue(text)))
}

// Step prints a progress step.
func Step(text string) {
	fmt.Fprintln(Out, Blue("→ ")+text)
}

// OK prints a success line.
func OK(text string) {
	fmt.Fprintln(Out, Mint("✓ ")+text)
}

// BigOK prints a high-visibility success block so the moment isn’t lost in noise.
func BigOK(text string) {
	Blank()
	width := len(text) + 6
	if width < 36 {
		width = 36
	}
	if width > 52 {
		width = 52
	}
	bar := strings.Repeat("─", width)
	fmt.Fprintln(Out, Mint("╭"+bar))
	fmt.Fprintln(Out, Mint("│  ✓  ")+Bold(Mint(text)))
	fmt.Fprintln(Out, Mint("╰"+bar))
	Blank()
}

// BigFail prints a high-visibility failure block (same weight as BigOK, coral).
func BigFail(text string) {
	Blank()
	width := len(text) + 6
	if width < 36 {
		width = 36
	}
	if width > 52 {
		width = 52
	}
	bar := strings.Repeat("─", width)
	fmt.Fprintln(Out, Coral("╭"+bar))
	fmt.Fprintln(Out, Coral("│  ✗  ")+Bold(Coral(text)))
	fmt.Fprintln(Out, Coral("╰"+bar))
	Blank()
}

// Warn prints a warning.
func Warn(text string) {
	fmt.Fprintln(Out, Peach("! ")+text)
}

// Fail prints an error line (no exit).
func Fail(text string) {
	fmt.Fprintln(Err, Coral("✗ ")+text)
}

// Info prints a neutral info line.
func Info(text string) {
	fmt.Fprintln(Out, Dim("· ")+text)
}

// Detail prints indented secondary text.
func Detail(text string) {
	fmt.Fprintln(Out, "  "+Dim(text))
}

// KV prints a labeled field.
func KV(key, value string) {
	fmt.Fprintf(Out, "  %s  %s\n", Pink(padKey(key)), value)
}

// Blank prints an empty line.
func Blank() {
	fmt.Fprintln(Out)
}

// SummaryBox prints a short end-of-command summary.
func SummaryBox(lines ...string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(Out, Blue("╭"+strings.Repeat("─", 36)))
	for _, line := range lines {
		fmt.Fprintln(Out, Blue("│ ")+line)
	}
	fmt.Fprintln(Out, Blue("╰"+strings.Repeat("─", 36)))
}

// ErrorMessage prints a friendly multi-line error for non-technical users.
func ErrorMessage(title string, detail string, tips ...string) {
	Fail(title)
	if detail != "" {
		fmt.Fprintln(Err, "  "+Dim(detail))
	}
	for _, t := range tips {
		fmt.Fprintln(Err, "  "+Blue("tip: ")+t)
	}
}

// HelpBlock prints help with color accents.
func HelpBlock() {
	Banner()
	Blank()
	fmt.Fprintln(Err, Pink("What is this?"))
	fmt.Fprintln(Err, "  "+Brand()+" keeps your Minecraft server mods up to date")
	fmt.Fprintln(Err, "  and can start or stop the server for you.")
	Blank()
	fmt.Fprintln(Err, Pink("Start here"))
	fmt.Fprintln(Err, "  "+Blue("./pastel install <pack>")+"  Get a modpack (slug, Modrinth URL, or .mrpack link)")
	fmt.Fprintln(Err, "  "+Blue("./pastel")+"                 Home — status + if a new pack is available")
	fmt.Fprintln(Err, "  "+Blue("./pastel run")+"             Refresh pin, start server in the background")
	fmt.Fprintln(Err, "  "+Blue("./pastel console")+"         Live logs + type commands")
	fmt.Fprintln(Err, "  "+Blue("./pastel stop")+"            Stop the server safely")
	fmt.Fprintln(Err, "  "+Dim("  stop -pid N")+"          Kill a specific process (lost folder)")
	fmt.Fprintln(Err, "  "+Dim("  stop -orphans")+"       Kill servers whose folder was deleted")
	Blank()
	fmt.Fprintln(Err, Pink("Pack care"))
	fmt.Fprintln(Err, "  "+Blue("./pastel refresh")+"         Re-download files for your current pin")
	fmt.Fprintln(Err, "  "+Blue("./pastel update")+"          Pick a pack version and upgrade (Maven or Modrinth)")
	fmt.Fprintln(Err, "  "+Dim("  alias:")+" upgrade")
	fmt.Fprintln(Err, "  "+Blue("./pastel status")+"          Detailed status for this folder")
	Blank()
	fmt.Fprintln(Err, Pink("Install examples"))
	fmt.Fprintln(Err, "  "+Dim("./pastel install aristea"))
	fmt.Fprintln(Err, "  "+Dim("./pastel install https://modrinth.com/modpack/aristea"))
	fmt.Fprintln(Err, "  "+Dim("./pastel install https://…/pack.mrpack"))
	fmt.Fprintln(Err, "  "+Dim("./pastel install com.iamkaf.modpacks:forever-world:1.1.0 -repo https://maven.kaf.sh"))
	Blank()
	fmt.Fprintln(Err, Pink("More"))
	fmt.Fprintln(Err, "  "+Dim("aliases:")+" install→get/add · console→attach/logs/terminal")
	fmt.Fprintln(Err, "  "+Blue("./pastel run -f")+"          Foreground mode (debug)")
	fmt.Fprintln(Err, "  "+Blue("./pastel version")+"         Show "+Brand()+" version")
	fmt.Fprintln(Err, "  "+Blue("./pastel pack …")+"          Author/publish packs (for Kaf)")
	Blank()
	fmt.Fprintln(Err, Pink("Optional flags"))
	fmt.Fprintln(Err, "  "+Dim("-memory 4G")+"       Memory for install / run")
	fmt.Fprintln(Err, "  "+Dim("-repo URL")+"        Maven host for group:artifact:version pins")
	fmt.Fprintln(Err, "  "+Dim("-yes")+"             Skip confirmations (scripts)")
	fmt.Fprintln(Err, "  "+Dim("-config path")+"     Where your server.pastel file is")
	fmt.Fprintln(Err, "  "+Dim("-v")+"               Extra detail (for troubleshooting)")
	Blank()
	fmt.Fprintln(Err, Dim("Setup: drop ")+Brand()+Dim(" in a folder → ./pastel install <pack> → ./pastel run"))
	Blank()
	fmt.Fprintln(Err, Dim("By running your server with Pastel you are indicating your agreement"))
	fmt.Fprintln(Err, Dim("to Mojang's EULA (https://aka.ms/MinecraftEULA)."))
}

func padKey(key string) string {
	const w = 12
	if len(key) >= w {
		return key
	}
	return key + strings.Repeat(" ", w-len(key))
}
