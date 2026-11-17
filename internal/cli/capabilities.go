package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/capability"
	"github.com/dovholuknf/atrium/internal/link"
)

func capabilitiesCmd() *cobra.Command {
	var board string
	c := &cobra.Command{
		Use:   "capabilities",
		Short: "What each room can do: which, room, list, run, declared",
		Long: "Read from the hub, which keeps the newest probe result of every room. The rooms probe themselves, read only.\n" +
			"`declared` shows the hub's list, and `declared --set <file>` saves a new one from the hub's machine.",
	}
	boardFlag(c, &board)
	c.AddCommand(capWhichCmd(&board), capRoomCmd(&board), capListCmd(&board), capRunCmd(&board), capDeclaredCmd(&board))
	return c
}

func capView(board string) (link.CapabilityView, error) {
	var v link.CapabilityView
	err := backlogCall(board, http.MethodGet, "/_hub/capabilities", nil, &v)
	return v, err
}

func capWhichCmd(board *string) *cobra.Command {
	var fresh time.Duration
	c := &cobra.Command{
		Use:   "which <id>...",
		Short: "The rooms where every named capability passes",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := capView(*board)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			rooms := link.CapabilityWhich(v, args, fresh, time.Now())
			if len(rooms) == 0 {
				fmt.Fprintln(w, "no room has all of these passing")
			}
			for _, m := range rooms {
				state := "match"
				switch {
				case !m.Online:
					state = "offline"
				case m.Stale:
					state = "stale"
				}
				fmt.Fprintf(w, "%-20s %s\n", m.Room, state)
			}
			return nil
		},
	}
	c.Flags().DurationVar(&fresh, "fresh", 24*time.Hour, "how recent a pass must be")
	return c
}

func capRoomCmd(board *string) *cobra.Command {
	return &cobra.Command{
		Use:   "room <name>",
		Short: "Every capability of one room, newest result each",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := capView(*board)
			if err != nil {
				return err
			}
			w, n := cmd.OutOrStdout(), 0
			for _, r := range v.Results {
				if strings.EqualFold(r.Room, args[0]) {
					n++
					fmt.Fprintf(w, "%-24s %-8s %-12s %s\n", r.Cap, r.State, r.Version, r.Reason)
				}
			}
			if n == 0 {
				fmt.Fprintln(w, "no results for that room")
			}
			return nil
		},
	}
}

func capListCmd(board *string) *cobra.Command {
	var kind string
	c := &cobra.Command{
		Use:   "list",
		Short: "The declared capabilities",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := capView(*board)
			if err != nil {
				return err
			}
			for _, raw := range v.Caps {
				var c struct {
					ID   string `json:"id"`
					Kind string `json:"kind"`
				}
				if json.Unmarshal(raw, &c) == nil && (kind == "" || c.Kind == kind) {
					fmt.Fprintf(cmd.OutOrStdout(), "%-24s %s\n", c.ID, c.Kind)
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "reach, toolchain, build or access")
	return c
}

func capRunCmd(board *string) *cobra.Command {
	return &cobra.Command{
		Use:   "run <room> [id...]",
		Short: "Ask a room to run its probes now, all or the named ids",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{"room": {args[0]}}
			if len(args) > 1 {
				q.Set("cap", strings.Join(args[1:], ","))
			}
			var res struct {
				Asked     []string          `json:"asked"`
				Unreached map[string]string `json:"unreached"`
			}
			if err := backlogCall(*board, http.MethodPost, "/_hub/capabilities?"+q.Encode(), nil, &res); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "asked %s\n", strings.Join(res.Asked, ", "))
			for room, why := range res.Unreached {
				fmt.Fprintf(w, "%s did not take it: %s\n", room, why)
			}
			return nil
		},
	}
}

func capDeclaredCmd(board *string) *cobra.Command {
	var set string
	c := &cobra.Command{
		Use:   "declared",
		Short: "The hub's declared list, or --set <file> to save a new one",
		Long: "Prints the hub's list as YAML. --set checks the file as a room would read it, and the hub saves it only when\n" +
			"its version is higher than the one stored and the request comes from the hub's own machine.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			if set != "" {
				b, err := os.ReadFile(set)
				if err != nil {
					return err
				}
				if _, err := capability.Lint(b); err != nil {
					return err
				}
				var out link.CapabilityList
				if err := backlogCall(*board, http.MethodPut, "/_hub/capability-list", map[string]string{"yaml": string(b)}, &out); err != nil {
					return err
				}
				fmt.Fprintf(w, "saved version %d\n", out.Version)
				return nil
			}
			var out link.CapabilityList
			if err := backlogCall(*board, http.MethodGet, "/_hub/capability-list", nil, &out); err != nil {
				return err
			}
			if out.Version == 0 {
				fmt.Fprintln(w, "the hub declares nothing of its own, so rooms use the shipped list and their own additions")
				return nil
			}
			fmt.Fprint(w, out.YAML)
			return nil
		},
	}
	c.Flags().StringVar(&set, "set", "", "save this file as the hub's list")
	return c
}
