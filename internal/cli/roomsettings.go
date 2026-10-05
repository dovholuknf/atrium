package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
	"github.com/spf13/cobra"
)

// roomSettingKeys are the settings `room set` and `room get` will touch. Only git_root so far:
// provisioning needs it before the room first runs, and nothing else wants a verb.
var roomSettingKeys = map[string]string{
	"git_root": gitsync.SettingGitRoot,
}

// roomSettingCmds returns `room set` and `room get`. They work on a STOPPED room's database
// and refuse a running one, which holds the store. Run them before the first start or after
// stopping the room.
func roomSettingCmds() []*cobra.Command {
	var db string
	set := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a room setting (git_root) in a stopped room's database",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomSet(cmd, orDefault(db, defaultRoomDB()), args[0], args[1])
		},
	}
	get := &cobra.Command{
		Use:   "get <key>",
		Short: "Print a room setting (git_root) from a stopped room's database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomGet(cmd, orDefault(db, defaultRoomDB()), args[0])
		},
	}
	for _, c := range []*cobra.Command{set, get} {
		c.Flags().StringVar(&db, "db", "", "the room's database")
	}
	return []*cobra.Command{set, get}
}

func roomSettingKey(name string) (string, error) {
	if k, ok := roomSettingKeys[name]; ok {
		return k, nil
	}
	return "", fmt.Errorf("unknown room setting %q, known: git_root", name)
}

func roomSettingErr(db string, err error) error {
	switch {
	case errors.Is(err, store.ErrDatabaseInUse):
		return fmt.Errorf("the room is running on %s: stop it first, then set the value", db)
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("no room database at %s: the room has not run here yet (or pass --db)", db)
	}
	return err
}

func runRoomSet(cmd *cobra.Command, db, name, value string) error {
	key, err := roomSettingKey(name)
	if err != nil {
		return err
	}
	value = strings.TrimSpace(value)
	if key == gitsync.SettingGitRoot && value != "" && !filepath.IsAbs(value) {
		return fmt.Errorf("git_root must be an absolute path, got %q", value)
	}
	if err := store.SetSettingOfFile(db, key, value); err != nil {
		return roomSettingErr(db, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\n", name, value)
	return nil
}

func runRoomGet(cmd *cobra.Command, db, name string) error {
	key, err := roomSettingKey(name)
	if err != nil {
		return err
	}
	v, err := store.SettingOfFile(db, key)
	if err != nil {
		return roomSettingErr(db, err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), v)
	return nil
}
