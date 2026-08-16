package main

import (
	"os"

	"github.com/iamkaf/pastel/internal/buildinfo"
	"github.com/iamkaf/pastel/internal/cli"
)

func main() {
	cli.Version = buildinfo.Version
	if err := cli.Run(os.Args[1:]); err != nil {
		if !cli.IsSilent(err) {
			// Fallback for errors that bypassed friendly formatting.
			os.Stderr.WriteString("pastel: " + err.Error() + "\n")
		}
		os.Exit(1)
	}
}
