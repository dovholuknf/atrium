// Package resources is the hand-edited inventory of what an agent may use on a machine: the file `resources.md`
// beside atrium's state dir. See docs/fabric/f-003-resources-design.md.
//
// ATRIUM HOLDS THE NAME OF A HOST, IDENTITY OR COMMAND THAT HAS A CREDENTIAL, NEVER THE CREDENTIAL. Nothing here
// writes the file except `Init`, which writes the starter and never over a file that is there.
package resources

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// File is the inventory's name inside the state dir.
const File = "resources.md"

// MaxBytes bounds what a read returns, so a file that grew wrong cannot fill a card's context.
const MaxBytes = 64 << 10

// Starter is what `atrium resources init` writes.
const Starter = `<!-- atrium resources for this room. Names and commands only. Never a token, password or key. -->
<!-- One "## <name>" heading per entry, then free prose. Agents read this through atrium_resources. -->
<!-- Say what the entry is, how to reach it, what is installed and where, and what it is good for. -->
<!-- Suggested working-directory rule: one directory per card, ~/work/<card alias>. -->

## example-machine
ssh example-machine. OS and arch. What is installed and where (PATH prefixes, vcpkg root). What it is good for.
Build in ~/work/<your alias>. Delete this entry.
`

// Path is where the inventory lives for a state dir.
func Path(stateDir string) string { return filepath.Join(stateDir, File) }

// Init writes the starter file and reports its path. A file that is already there is left exactly as it is, and
// the error says so.
func Init(stateDir string) (string, error) {
	p := Path(stateDir)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return p, err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return p, fmt.Errorf("%s already exists. it was not changed", p)
		}
		return p, err
	}
	if _, err := f.WriteString(Starter); err != nil {
		_ = f.Close()
		return p, err
	}
	return p, f.Close()
}

// Read is the file's text, whether it exists, and whether it was cut at MaxBytes. A missing file is not an error.
func Read(stateDir string) (text string, exists, cut bool, err error) {
	b, err := os.ReadFile(Path(stateDir))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	if len(b) > MaxBytes {
		b, cut = b[:MaxBytes], true
	}
	return string(b), true, cut, nil
}
