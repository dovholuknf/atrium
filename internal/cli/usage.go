package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/store"
	"github.com/spf13/cobra"
)

// `atrium usage backfill`.

func usageCmd() *cobra.Command {
	c := &cobra.Command{Use: "usage", Short: "Token use on record"}
	c.AddCommand(usageBackfillCmd())
	return c
}

// parseSince reads a span like 30d, 12h or 90m.
func parseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.ParseFloat(n, 64)
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("--since %q is not a span like 30d", s)
		}
		return time.Duration(days * 24 * float64(time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("--since %q is not a span like 30d", s)
	}
	return d, nil
}

func usageBackfillCmd() *cobra.Command {
	var (
		db, since string
		dry       bool
	)
	c := &cobra.Command{
		Use:   "backfill",
		Short: "Write usage rows for turns the room did not record, from transcripts",
		Long: "A one-time command, never run on a timer. Reads the transcript of every Claude\n" +
			"card's known resume id and writes usage rows with cause `backfill` for replies\n" +
			"no row already covers, so running it again adds nothing. It goes back at most\n" +
			"30 days.\n\n" +
			"Replies are grouped into rows by the quiet between them, and subagent\n" +
			"transcripts are not read. The department and director on a backfilled row come\n" +
			"from the card as it is NOW, since that is all there is.\n\n" +
			"The room's database is written directly. Use --dry-run to see what it would add.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			span, err := parseSince(since)
			if err != nil {
				return err
			}
			if db == "" {
				db = defaultRoomDB()
			}
			st, err := store.Open(db)
			if err != nil {
				return fmt.Errorf("open %s: %w", db, err)
			}
			defer st.Close()
			res, err := daemon.BackfillUsage(st, time.Now().UTC().Add(-span), dry, nil)
			if err != nil {
				return err
			}
			verb := "wrote"
			if dry {
				verb = "would write"
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%s %d rows for %d replies on %d cards (%d cards had no transcript)\n",
				verb, res.Rows, res.Replies, res.Cards, res.Skipped)
			fmt.Fprintln(w, "the department and director on these rows are the card's as it is now.")
			return nil
		},
	}
	c.Flags().StringVar(&db, "db", "", "the room's database")
	c.Flags().StringVar(&since, "since", "30d", "how far back to read, at most 30d")
	c.Flags().BoolVar(&dry, "dry-run", false, "count what would be written and write nothing")
	return c
}
