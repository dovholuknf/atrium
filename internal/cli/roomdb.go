package cli

import (
	"fmt"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
	"github.com/spf13/cobra"
)

// `atrium db compact`.
//
// A room's database only grows unless it was made in incremental auto_vacuum
// mode, and an existing file can only change mode with a full rebuild, which a
// running room cannot allow. So the rebuild is done offline, into a new file,
// and swapping it in is left to the operator with the room stopped.

func dbCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "db",
		Short: "Offline tools for a room's database",
	}
	c.AddCommand(dbCompactCmd())
	return c
}

func dbCompactCmd() *cobra.Command {
	var (
		in, out     string
		windowBytes int64
		dropKinds   string
	)
	c := &cobra.Command{
		Use:   "compact --in <db> --out <db>",
		Short: "Write a packed copy of a database, in incremental auto_vacuum mode",
		Long: "Copies --in to --out with VACUUM INTO, optionally trims the copy, then\n" +
			"switches the copy to incremental auto_vacuum so a room opened on it hands\n" +
			"freed space back to disk.\n\n" +
			"--in is never modified and never replaced. --out must not exist. A database\n" +
			"that anything has open, a running room included, is refused: stop the room,\n" +
			"or compact a copy of the file.\n\n" +
			"--window-bytes keeps only each card's newest events up to that many bytes of\n" +
			"payload, as event_window_bytes does live. --drop-kinds removes every event of\n" +
			"the named kinds, as event_cold_kinds does live; created and submitted cannot\n" +
			"be dropped. Rows removed from the copy are still in --in.\n\n" +
			"To use the result, stop the room, move the old file and its -wal/-shm aside,\n" +
			"and put the copy in its place.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if in == "" || out == "" {
				return fmt.Errorf("--in and --out are both required")
			}
			var kinds []string
			for _, k := range strings.Split(dropKinds, ",") {
				if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
					kinds = append(kinds, k)
				}
			}
			res, err := store.Compact(in, out, store.CompactOptions{WindowBytes: windowBytes, DropKinds: kinds})
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%s  %8s  (with -wal)\n", in, size(res.InBytes))
			fmt.Fprintf(w, "%s  %8s  incremental auto_vacuum\n", out, size(res.OutBytes))
			if res.EventsAfter != res.EventsBefore {
				fmt.Fprintf(w, "events %d -> %d on the copy; the rest are still in %s\n",
					res.EventsBefore, res.EventsAfter, in)
			}
			return nil
		},
	}
	c.Flags().StringVar(&in, "in", "", "the database to read")
	c.Flags().StringVar(&out, "out", "", "where to write the copy; must not exist")
	c.Flags().Int64Var(&windowBytes, "window-bytes", 0, "keep each card's newest events up to this many payload bytes")
	c.Flags().StringVar(&dropKinds, "drop-kinds", "", "comma-separated event kinds to remove from the copy")
	return c
}
