package main

import (
	"fmt"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/spf13/cobra"
)

// `atrium2 hub backups` and `atrium2 hub restore`.
//
// ── why these are commands rather than a pane ───────────
//
// A restore is what somebody reaches for when the board will not come up, which
// is exactly when a page served by the thing that will not come up is no use.
// It has to work from a terminal with nothing else running.
//
// The listing is a command for the same reason: it is the thing you read
// immediately before restoring, and reading it somewhere else would mean typing
// a timestamp you saw in another window.

func hubBackupsCmd() *cobra.Command {
	var f hubStoreFlags
	c := &cobra.Command{
		Use:     "backups",
		Aliases: []string{"backup"},
		Short:   "The snapshots this hub has taken of its own store",
		Long: "A running hub snapshots its store every ten minutes and keeps them in tiers:\n" +
			"everything from the last hour, one an hour for a day, one a day for a week,\n" +
			"and one a week for a month.\n\n" +
			"It holds no work, so a snapshot is the room list, their names, their join\n" +
			"secrets, what is marked for deletion, and what each room last said it was\n" +
			"holding. Nothing here is a session and nothing here is an agent.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := backupsIn(f.keys().Dir, f.db)
			list, err := hubstore.Backups(dir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintf(out, "no snapshots in %s yet. a running hub writes one every %s\n",
					dir, hubstore.BackupEvery)
				return nil
			}
			for _, b := range list {
				fmt.Fprintf(out, "%s  %8s  %s\n",
					b.At.Local().Format("2006-01-02 15:04:05"), size(b.Size), b.Path)
			}
			fmt.Fprintf(out, "\n%d snapshot(s) in %s. the newest is %s old.\n",
				len(list), dir, list[0].Age().Round(time.Second))
			fmt.Fprintln(out, "`atrium2 hub restore <path>` puts one back, with the hub stopped.")
			return nil
		},
	}
	f.bind(c)
	return c
}

func hubRestoreCmd() *cobra.Command {
	var f hubStoreFlags
	c := &cobra.Command{
		Use:   "restore <snapshot>",
		Short: "Put a snapshot back, with the hub stopped",
		Long: "Replaces the hub's store with one of its own snapshots.\n\n" +
			"WHAT IS THERE NOW IS MOVED ASIDE, not deleted, and the name it is moved to\n" +
			"is printed. Restoring is done under pressure from a list of timestamps, and\n" +
			"the wrong one is one keypress away, so undoing a restore is another restore.\n\n" +
			"The snapshot is opened and checked before anything is moved. Stop the hub\n" +
			"first: a hub running while its database is swapped underneath carries on\n" +
			"serving the old one and writes it back over the new.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db := f.path()
			aside, err := hubstore.Restore(db, args[0])
			out := cmd.OutOrStdout()
			if aside != "" {
				fmt.Fprintf(out, "what was there is at %s\n", aside)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "%s is now %s\n", db, args[0])
			fmt.Fprintln(out, "start the hub. every room reconnects on its own and says what "+
				"it is holding, so anything the snapshot remembers wrongly is corrected "+
				"the moment that room is back.")
			return nil
		},
	}
	f.bind(c)
	return c
}

// size is a file size somebody reads rather than parses.
func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%dKB", n/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
