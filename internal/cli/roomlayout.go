package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/link"
)

// roomLayout is one room's folders under the machine's root: <root>/<room>/config holds the database and everything
// the room keeps beside it, logs the room's output, cards its card homes. A zero layout is a room started the old way,
// by --dir and --db.
type roomLayout struct{ root, name string }

// rootEnv names the machine's root when --root is not given.
const rootEnv = "ATRIUM_ROOT"

func (l roomLayout) on() bool             { return l.name != "" }
func (l roomLayout) path(s string) string { return filepath.Join(l.root, l.name, s) }
func (l roomLayout) config() string       { return l.path("config") }
func (l roomLayout) db() string           { return filepath.Join(l.config(), "atrium.db") }
func (l roomLayout) logs() string         { return l.path("logs") }
func (l roomLayout) cards() string        { return l.path("cards") }

// newRoomLayout is the layout for --name and --root. No name is the zero layout. A name needs a root, from the flag or
// ATRIUM_ROOT, and must be a room name as the hub takes it, so it cannot climb out of the root.
func newRoomLayout(root, name string) (roomLayout, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return roomLayout{}, nil
	}
	if roomNameOf(name) != name {
		return roomLayout{}, fmt.Errorf("%q is not a room name: letters, digits, '-', '_' and '.' only", name)
	}
	root = strings.TrimSpace(orDefault(root, os.Getenv(rootEnv)))
	if root == "" {
		return roomLayout{}, fmt.Errorf("--name %s needs --root or %s, the folder every room on this machine lives under",
			name, rootEnv)
	}
	if !filepath.IsAbs(root) {
		return roomLayout{}, fmt.Errorf("the root is an absolute path, not %q", root)
	}
	return roomLayout{root: filepath.Clean(root), name: name}, nil
}

// checkJoined refuses a key folder that belongs to another room, so --name cannot start one room under another's name.
func (l roomLayout) checkJoined(keys link.Keys) error {
	if !l.on() {
		return nil
	}
	saved, err := keys.Joined()
	if err != nil {
		return err
	}
	if !strings.EqualFold(saved.Room, l.name) {
		return fmt.Errorf("%s holds room %q, not %q", keys.Dir, saved.Room, l.name)
	}
	return nil
}

var errNoName = errors.New("--name is required")
