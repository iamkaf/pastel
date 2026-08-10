package runtime

import (
	"fmt"

	"github.com/iamkaf/pastel/internal/ui"
)

func printBackgroundReady() {
	ui.BigOK("Your server is running in the background")
	ui.Title("What you can do now")
	fmt.Fprintf(ui.Out, "  %s  %s\n", ui.Blue(padRunCmd("./pastel console")), "See the live log and type commands")
	fmt.Fprintf(ui.Out, "  %s  %s\n", ui.Blue(padRunCmd("./pastel stop")), "Shut the server down when you're done")
	fmt.Fprintf(ui.Out, "  %s  %s\n", ui.Blue(padRunCmd("./pastel")), "Check status anytime")
	ui.Blank()
	ui.Detail("You can close this terminal — the server keeps going.")
}
