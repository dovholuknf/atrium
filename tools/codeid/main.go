// Command codeid prints the room code fingerprint of the repository in the current directory. The Makefile and the
// deploy scripts stamp it into the binary. See internal/codeid.
package main

import (
	"fmt"
	"os"

	"github.com/dovholuknf/atrium/internal/codeid"
)

func main() {
	id, err := codeid.Compute(os.DirFS("."))
	if err != nil {
		fmt.Fprintln(os.Stderr, "codeid:", err)
		os.Exit(1)
	}
	fmt.Println(id)
}
