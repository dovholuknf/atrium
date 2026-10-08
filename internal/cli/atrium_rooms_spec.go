package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/spf13/cobra"
)

// A room's spec and lock, on the hub's side and the room's. See internal/link/roomspec.go.

// readSpecArg reads a spec from a file, or from standard input for "-", at most one byte over the bound so that the
// size check can say it is too big.
func readSpecArg(cmd *cobra.Command, path string) ([]byte, error) {
	var r io.Reader = cmd.InOrStdin()
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	return io.ReadAll(io.LimitReader(r, hubstore.MaxRoomSpec+1))
}

// specBy is who a spec is recorded as set by.
func specBy() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return "cli"
}

// roomSpecCmd is `rooms spec get|set`.
func roomSpecCmd(prefix string) *cobra.Command {
	c := &cobra.Command{
		Use:   "spec",
		Short: "What a room should be: the room.yaml the hub keeps for it",
		Long: "The hub keeps each room's desired room.yaml verbatim, with its sha256 and who set it. The room\n" +
			"fetches it with `atrium room spec pull`. A spec holds no credentials, and its `name` is the room's.",
	}
	var gf, sf hubStoreFlags
	get := &cobra.Command{
		Use:   "get <name>",
		Short: "Print a room's spec",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := gf.open()
			if err != nil {
				return err
			}
			defer store.Close()
			sp, err := store.RoomSpecOf(args[0])
			if err != nil {
				return knownRooms(store, args[0], err)
			}
			fmt.Fprint(cmd.OutOrStdout(), sp.YAML)
			return nil
		},
	}
	gf.bind(get, prefix)
	set := &cobra.Command{
		Use:   "set <name> <file|->",
		Short: "Replace a room's spec with a file, or with standard input for -",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := readSpecArg(cmd, args[1])
			if err != nil {
				return err
			}
			store, err := sf.open()
			if err != nil {
				return err
			}
			defer store.Close()
			sp, err := store.SetRoomSpec(args[0], raw, specBy())
			if err != nil {
				return knownRooms(store, args[0], err)
			}
			sf.nudge()
			fmt.Fprintf(cmd.OutOrStdout(), "set the spec for %s, sha256 %s\n", sp.Room, sp.SHA256)
			return nil
		},
	}
	sf.bind(set, prefix)
	sf.bindBoard(set, prefix)
	c.AddCommand(get, set)
	return c
}

// roomLockCmd is `rooms lock get`.
func roomLockCmd(prefix string) *cobra.Command {
	c := &cobra.Command{
		Use:   "lock",
		Short: "What a room reports it is: the room.lock it last posted",
	}
	var f hubStoreFlags
	get := &cobra.Command{
		Use:   "get <name>",
		Short: "Print a room's latest lock",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := f.open()
			if err != nil {
				return err
			}
			defer store.Close()
			lk, err := store.RoomLockOf(args[0])
			if err != nil {
				return knownRooms(store, args[0], err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(lk.Lock))
			return nil
		},
	}
	f.bind(get, prefix)
	c.AddCommand(get)
	return c
}

// ── the room's side ─────────────────────────────────────

// roomLinkFor is the room's name and a dialer to the hub it joined, from the keys in dir.
func roomLinkFor(dir string) (string, link.Dialer, error) {
	keys := link.Keys{Dir: orDefault(dir, roomDir())}
	saved, err := keys.Joined()
	if err != nil {
		return "", nil, err
	}
	d, err := roomDialer(link.Join{
		Transport: saved.Transport, Addr: saved.Hub, Service: saved.Service, ShareToken: saved.Share,
	}, keys, saved.Identity)
	if err != nil {
		return "", nil, err
	}
	return saved.Room, d, nil
}

// PostRoomLock sends a room.lock (a JSON object with "version": 1) to the hub this machine joined, over the room's
// mutual TLS link, as the room the certificate names. dir is the room's key directory, "" for the default. The hub
// replaces its last lock and never changes the spec. For `atrium room setup` to call when it has applied a spec.
func PostRoomLock(ctx context.Context, dir string, lock []byte) error {
	room, d, err := roomLinkFor(dir)
	if err != nil {
		return err
	}
	return link.PostRoomLock(ctx, d, room, lock)
}

// roomSpecPullCmd is `room spec pull`.
func roomSpecPullCmd() *cobra.Command {
	var dir, out string
	pull := &cobra.Command{
		Use:   "pull",
		Short: "Fetch this room's spec from the hub and write it",
		Long: "Asks the hub, over this room's own link, for the room.yaml the operator set for it. Writes it to\n" +
			"--out, or to room.yaml beside the room's certificate. With --out - it goes to standard output.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			room, d, err := roomLinkFor(dir)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Minute)
			defer cancel()
			raw, sum, err := link.FetchRoomSpec(ctx, d, room)
			if errors.Is(err, link.ErrNoRoomSpec) {
				return fmt.Errorf("the hub has no spec for %s. `%s %s <file>` on the hub gives it one",
					room, atriumCmd("rooms spec set"), room)
			}
			if err != nil {
				return err
			}
			if out == "-" {
				_, err = cmd.OutOrStdout().Write(raw)
				return err
			}
			if out == "" {
				out = filepath.Join(orDefault(dir, roomDir()), "room.yaml")
			}
			if err := os.WriteFile(out, raw, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s, sha256 %s\n", out, sum)
			return nil
		},
	}
	pull.Flags().StringVar(&dir, "dir", "", "where this room keeps its certificate")
	pull.Flags().StringVar(&out, "out", "", "where to write the spec (- for standard output)")
	c := &cobra.Command{Use: "spec", Short: "This room's spec, from the hub"}
	c.AddCommand(pull)
	return c
}
