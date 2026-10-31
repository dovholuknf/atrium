package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/dblock"
	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link/transport"
	"github.com/dovholuknf/atrium/internal/store"
	"github.com/spf13/cobra"
)

// `atrium room move`: a stopped room's key folder and database, with everything kept beside the database, copied into
// <root>/<room>/config. The old folders are left for the operator to delete. Card homes are not copied: each card row
// holds its home's absolute path.

func roomMoveCmd() *cobra.Command {
	var dir, db, root, name string
	c := &cobra.Command{
		Use:   "move --name <room> --root <folder>",
		Short: "Copy a stopped room into <root>/<room>, where `atrium room --name` looks for it",
		Long: "Copies the room's key folder (--dir) and its database (--db), with everything kept beside the\n" +
			"database, into <root>/<room>/config, and its logs into <root>/<room>/logs. The database is copied\n" +
			"with VACUUM INTO and the copy must hold as many cards as the original.\n\n" +
			"The room must be stopped, and its pty host too: a pty host is found by its folder, so the one\n" +
			"running for the old folder is not the moved room's. Nothing old is changed or removed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return errNoName
			}
			lay, err := newRoomLayout(root, name)
			if err != nil {
				return err
			}
			return moveRoom(cmd.OutOrStdout(), transport.Keys{Dir: orDefault(dir, roomDir())}, orDefault(db, defaultRoomDB()),
				lay)
		},
	}
	c.Flags().StringVar(&name, "name", "", "the room's name, as its room.json says")
	c.Flags().StringVar(&root, "root", "", "the folder every room on this machine lives under (default $"+rootEnv+")")
	c.Flags().StringVar(&dir, "dir", "", "the room's key folder today")
	c.Flags().StringVar(&db, "db", "", "the room's database today")
	return c
}

func moveRoom(w io.Writer, keys transport.Keys, db string, lay roomLayout) error {
	if err := lay.checkJoined(keys); err != nil {
		return err
	}
	db = filepath.Clean(filepath.FromSlash(db))
	if entries, err := os.ReadDir(lay.config()); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already holds files. move never writes over a room", lay.config())
	}
	hold, err := dblock.Acquire(db)
	if err != nil {
		return fmt.Errorf("stop the room first: %w", err)
	}
	defer hold.Release()
	before := daemon.CountCards(db)
	if before < 0 {
		return fmt.Errorf("could not read the cards in %s", db)
	}

	for _, d := range []string{lay.config(), lay.logs()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	dbDir, dbName := filepath.Dir(db), filepath.Base(db)
	sameDir := sameFolder(keys.Dir, dbDir)
	keyEntries, err := os.ReadDir(keys.Dir)
	if err != nil {
		return err
	}
	for _, e := range keyEntries {
		n := e.Name()
		switch {
		case strings.HasPrefix(n, dbName), n == "daemon.json", n == "lastdb.json":
		case isRoomLog(n):
			if err := copyNew(filepath.Join(keys.Dir, n), filepath.Join(lay.logs(), n)); err != nil {
				return err
			}
		case sameDir && isRoomData(n):
		default:
			if err := copyNew(filepath.Join(keys.Dir, n), filepath.Join(lay.config(), n)); err != nil {
				return err
			}
		}
	}
	for _, n := range daemon.RoomDataNames {
		if err := copyNew(filepath.Join(dbDir, n), filepath.Join(lay.config(), n)); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if !sameDir {
		if err := copyNew(filepath.Join(dbDir, "ptyhost.log"), filepath.Join(lay.logs(), "ptyhost.log")); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	if _, err := store.Compact(db, lay.db(), store.CompactOptions{}); err != nil {
		return fmt.Errorf("copying the database: %w", err)
	}
	if after := daemon.CountCards(lay.db()); after != before {
		return fmt.Errorf("the copy holds %d cards and %s holds %d. %s is not ready to start", after, db, before,
			lay.config())
	}
	fmt.Fprintf(w, "  moved room %s: %d cards\n", lay.name, before)
	fmt.Fprintf(w, "  config %s\n  logs   %s\n", lay.config(), lay.logs())
	fmt.Fprintf(w, "  start it with: atrium room --root %s --name %s\n", lay.root, lay.name)
	fmt.Fprintf(w, "  left in place, delete once the moved room runs: %s and %s with its -wal and -shm\n", keys.Dir, db)
	return nil
}

func isRoomLog(n string) bool {
	return strings.HasSuffix(n, ".log") || strings.HasPrefix(n, "room.err") || strings.HasPrefix(n, "room.out")
}

func isRoomData(n string) bool {
	for _, d := range daemon.RoomDataNames {
		if strings.EqualFold(n, d) {
			return true
		}
	}
	return false
}

func sameFolder(a, b string) bool {
	x, err1 := filepath.Abs(a)
	y, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && strings.EqualFold(filepath.Clean(x), filepath.Clean(y))
}

// copyNew copies a file or a folder tree, merging folders and refusing any file that already exists, so two sources
// can never overwrite each other. A link is refused rather than followed out of the room.
func copyNew(from, to string) error {
	fi, err := os.Lstat(from)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%s is a link. copy what it points at by hand", from)
	case fi.IsDir():
		if err := os.Mkdir(to, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		entries, err := os.ReadDir(from)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyNew(filepath.Join(from, e.Name()), filepath.Join(to, e.Name())); err != nil {
				return err
			}
		}
		return nil
	case !fi.Mode().IsRegular():
		return fmt.Errorf("%s is not a file or a folder", from)
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
