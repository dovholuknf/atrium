// Command atrium2 is a TEMPORARY shim over the one atrium binary, so this
// machine's live scripts keep running `atrium2 hub` and `atrium2 room` until
// the cutover. Deleted in stage 4 of docs/one-atrium-plan.md.
package main

import (
	"os"

	"github.com/dovholuknf/atrium/internal/cli"
)

func main() {
	os.Exit(cli.ExecuteAtrium2())
}
