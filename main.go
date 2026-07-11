// Command bisturi surgically cuts topics out of a Claude Code session
// transcript, with a TUI, safe re-threading, and restorable surgeries.
package main

import (
	"os"

	"github.com/wagnersilva/ctx-bisturi/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:]))
}
