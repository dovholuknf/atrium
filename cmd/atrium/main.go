package main

import (
	"os"

	"github.com/dovholuknf/atrium/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
