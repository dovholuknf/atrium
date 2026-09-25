package cli

import (
	"encoding/json"
	"fmt"

	"github.com/dovholuknf/atrium/internal/store"
	"github.com/spf13/cobra"
)

// `atrium ledger`.
//
// The room rewrites work-ledger.md beside its database on every change. When
// the room died between a change and that rewrite the file is stale, and this
// prints the same list from the database itself, opened read only.

func ledgerCmd() *cobra.Command {
	var (
		db     string
		asJSON bool
	)
	c := &cobra.Command{
		Use:   "ledger",
		Short: "List delegated work from a room's database, read only",
		Long: "Prints the work ledger: every work item that is not closed, the ones that\n" +
			"ended without a report first, then the ones closed in the last seven days.\n\n" +
			"The database is opened read only, with no migration, so this is safe against\n" +
			"a running room and against one that is down. It prints what work-ledger.md\n" +
			"beside the database says, and is the answer when that file is stale.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if db == "" {
				db = defaultRoomDB()
			}
			v, err := store.ReadLedger(db)
			if err != nil {
				return fmt.Errorf("read the ledger from %s: %w", db, err)
			}
			w := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(v)
			}
			fmt.Fprint(w, store.RenderLedger(v))
			return nil
		},
	}
	c.Flags().StringVar(&db, "db", "", "the room's database")
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON for a script")
	return c
}
