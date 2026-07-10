package main

import (
	"os"

	"github.com/iamkaf/pastel/internal/cli"
)

// Set via -ldflags "-X main.version=1.0.0"
var version = "0.1.0-dev"

func main() {
	cli.Version = version
	if err := cli.Run(os.Args[1:]); err != nil {
		if !cli.IsSilent(err) {
			// Fallback for errors that bypassed friendly formatting.
			os.Stderr.WriteString("pastel: " + err.Error() + "\n")
		}
		os.Exit(1)
	}
}
