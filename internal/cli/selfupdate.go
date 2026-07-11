package cli

import (
	"fmt"

	"github.com/iamkaf/pastel/internal/selfupdate"
	"github.com/iamkaf/pastel/internal/ui"
)

func cmdSelfUpdate(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("self-update does not accept arguments")
	}
	ui.Banner()
	ui.Blank()
	ui.Title("Updating Pastel")
	ui.Step("Downloading the latest release…")
	result, err := selfupdate.Run(selfupdate.Options{})
	if err != nil {
		return fmt.Errorf("self-update failed: %w", err)
	}
	version := result.Tag
	if version == "" {
		version = "the latest release"
	}
	ui.BigOK("Pastel is now " + version)
	ui.Detail("Restart any running Pastel command to use the new version.")
	return nil
}
