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

// roomSettingKeys are the settings `room set` and `room get` will touch: the folders provisioning needs before the room
// first runs (git_root, scm_root, and the work-root pair reviews_root and context_handoff_dir), and nothing else wants
// a verb.
var roomSettingKeys = map[string]string{
	"git_root":            gitsync.SettingGitRoot,
	"scm_root":            gitsync.SettingSCMRoot,
	"reviews_root":        store.SettingReviewsRoot,
	"context_handoff_dir": store.SettingContextHandoffDir,
}

const roomSettingNames = "git_root, scm_root, reviews_root, context_handoff_dir"

// roomSettingCmds returns `room set` and `room get`. They work on a STOPPED room's database
// and refuse a running one, which holds the store. Run them before the first start or after
// stopping the room.
func roomSettingCmds() []*cobra.Command {
	var db string
	set := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a room setting (" + roomSettingNames + ") in a stopped room's database",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomSet(cmd, orDefault(db, defaultRoomDB()), args[0], args[1])
		},
	}
	get := &cobra.Command{
		Use:   "get <key>",
		Short: "Print a room setting (" + roomSettingNames + ") from a stopped room's database",
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
	return "", fmt.Errorf("unknown room setting %q, known: %s", name, roomSettingNames)
}

// worded is the verb's own sentence, which still unwraps to the store's error so a caller can tell what it was.
type worded struct {
	msg string
	err error
}

func (w worded) Error() string { return w.msg }
func (w worded) Unwrap() error { return w.err }

func roomSettingErr(db string, err error) error {
	switch {
	case errors.Is(err, store.ErrDatabaseInUse):
		return worded{fmt.Sprintf("the room is running on %s: stop it first, then set the value", db), err}
	case errors.Is(err, os.ErrNotExist):
		return worded{fmt.Sprintf("no room database at %s: the room has not run here yet (or pass --db)", db), err}
	}
	return err
}

func runRoomSet(cmd *cobra.Command, db, name, value string) error {
	value, err := storeRoomSetting(db, name, value)
	if err != nil {
		// the verb words it itself and does not hand on the store's error (setup does, to tell a running room apart)
		var w worded
		if errors.As(err, &w) {
			return errors.New(w.msg)
		}
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\n", name, value)
	return nil
}

// storeRoomSetting is `room set` without the printing: it checks the value as `room set` does, stores it, and returns what was stored.
func storeRoomSetting(db, name, value string) (string, error) {
	key, err := roomSettingKey(name)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if key == gitsync.SettingGitRoot && value != "" && !filepath.IsAbs(value) {
		return "", fmt.Errorf("git_root must be an absolute path, got %q", value)
	}
	if key == gitsync.SettingSCMRoot && value != "" && !filepath.IsAbs(gitsync.ExpandHome(value)) {
		return "", fmt.Errorf("scm_root must be an absolute path or start with ~/, got %q", value)
	}
	if key == store.SettingContextHandoffDir {
		// The same check the settings API makes: trimmed, absolute, cleaned.
		v, err := store.CheckContextHandoffDir(value)
		if err != nil {
			return "", err
		}
		value = v
	}
	if key == store.SettingReviewsRoot && value != "" {
		if !filepath.IsAbs(value) {
			return "", fmt.Errorf("reviews_root must be an absolute path, got %q", value)
		}
		value = filepath.Clean(value)
	}
	if err := store.SetSettingOfFile(db, key, value); err != nil {
		return "", roomSettingErr(db, err)
	}
	return value, nil
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
