package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/requirements"
)

// newRequirements is `atrium requirements <file> --json`.
//
// The binary is the one parser of atrium.requirements.yaml so the schema has one
// set of rules and one set of messages. scripts/room-check.ps1 reads the JSON.
// It reads and prints. It runs no check, touches no room, and learns no git.
func newRequirements() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "requirements <file>",
		Short: "Parse an atrium.requirements.yaml and print it normalized",
		Long: "Reads a project's atrium.requirements.yaml and prints the normalized result as JSON.\n\n" +
			"A file with an unknown key, an absolute path, or anything that looks like a secret is\n" +
			"refused with the key and the line, and the exit status is 1.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			// One byte over the limit is enough to know, and nothing larger is read.
			data, err := io.ReadAll(io.LimitReader(f, requirements.MaxSize+1))
			if err != nil {
				return err
			}
			file, err := requirements.Parse(data)
			if err != nil {
				var bad *requirements.Error
				if errors.As(err, &bad) {
					for _, p := range bad.Problems {
						fmt.Fprintln(cmd.ErrOrStderr(), p)
					}
					cmd.SilenceErrors = true
					return errAlreadySaid
				}
				return err
			}
			if !asJSON {
				fmt.Fprintf(cmd.OutOrStdout(), "%s is valid. pass --json for the normalized form\n", args[0])
				return nil
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(file)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the normalized file as JSON")
	return c
}
