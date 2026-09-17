package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/spf13/cobra"
)

// `atrium2 hub room ...`: the inventory, from a terminal.
//
// ── why these run beside a hub rather than through it ────
//
// Every one of these opens the hub's store directly, including while a hub is
// running on it. SQLite in WAL mode is built for that, and the alternative
// would be an API on the hub that these commands call, which is a second way in
// to guard and a reason the commands stop working the moment the hub is down.
//
// Adding a room to a hub that is not running is a thing somebody will do, and
// it has to work.
//
// ── and why they exist at all when the board is coming ───
//
// The board grows this list next (item 3 in docs/decisions.md). These come
// first because every later piece needs a room to exist before it can be shown,
// and because a hub that can only be set up through its own web page is a hub
// nobody can script.

func hubRoomsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "room",
		Short: "The rooms this hub knows about: add, list, and remove",
		Long: "A room is a row somebody made on purpose. It appears the moment it is\n" +
			"added, before it has ever connected, and it stays there when it is offline.\n\n" +
			"THE HUB NAMES THE ROOM. `add` gives it a name and mints that name into the\n" +
			"join string, so the string authorises exactly one name and the machine that\n" +
			"pastes it does not choose what it is called.",
	}
	c.AddCommand(roomAddCmd(), roomListCmd(), roomTokenCmd(), roomMarkCmd(),
		roomRemoveCmd(), roomLogCmd())
	return c
}

// roomLogCmd is what makes wholesale replacement answerable.
//
// A room coming back replaces everything the hub was holding for it, which is
// the right rule and is also the one that can quietly lose something a person
// remembers seeing. This is where "I am sure there was a card there" stops
// being an argument and becomes a lookup.
//
// It outlives its subject. Forcing out a machine that is never coming back
// removes the room and leaves this, because the record of having done that is
// exactly what somebody wants afterwards.
func roomLogCmd() *cobra.Command {
	var f hubStoreFlags
	var limit int
	c := &cobra.Command{
		Use:   "log [name]",
		Short: "What has happened to this hub's rooms, newest first",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := f.open()
			if err != nil {
				return err
			}
			defer store.Close()

			var entries []hubstore.Entry
			if len(args) == 1 {
				// BY NAME, NOT BY LOOKING THE ROOM UP FIRST. Resolving the name
				// through the room list would mean this command stops working
				// at exactly the moment it is most wanted: after the room was
				// forced out and somebody is asking what happened to it.
				entries, err = store.AuditByName(args[0], limit)
			} else {
				entries, err = store.Audit(limit)
			}
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(entries) == 0 {
				if len(args) == 1 {
					fmt.Fprintf(out, "nothing has ever been written down about a room "+
						"called %q on this hub\n", args[0])
					return nil
				}
				fmt.Fprintln(out, "nothing has happened to a room on this hub yet")
				return nil
			}
			for _, e := range entries {
				fmt.Fprintf(out, "%s  %-12s %-20s %s\n",
					e.At.Local().Format("2006-01-02 15:04:05"), e.RoomName, e.Kind, e.Detail)
			}
			return nil
		},
	}
	f.bind(c)
	c.Flags().IntVar(&limit, "limit", 50, "how many lines")
	return c
}

// hubStoreFlags are the two every one of these needs.
type hubStoreFlags struct{ dir, db string }

func (f *hubStoreFlags) bind(c *cobra.Command) {
	c.Flags().StringVar(&f.dir, "dir", "", "where this hub keeps its certificates")
	c.Flags().StringVar(&f.db, "db", "", "the hub's own store (default: under --dir)")
}

func (f *hubStoreFlags) keys() link.Keys { return link.Keys{Dir: orDefault(f.dir, hubDir())} }

func (f *hubStoreFlags) open() (*hubstore.Store, error) {
	k := f.keys()
	return hubstore.Open(orDefault(f.db, filepath.Join(k.Dir, "hub.db")))
}

// roomAddCmd is the command decision 8 is about.
func roomAddCmd() *cobra.Command {
	var f hubStoreFlags
	var port, transport, service string
	c := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a room and print its join string",
		Long: "Writes the room down and prints the one line to paste on the machine that\n" +
			"will be that room.\n\n" +
			"THE JOIN STRING IS SHOWN ONCE. It is good once and for an hour. If it goes\n" +
			"missing, `atrium2 hub room token <name>` mints another and retires the old\n" +
			"one: the hub holds a hash and cannot show you what it printed before.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := f.open()
			if err != nil {
				return err
			}
			defer store.Close()

			r, err := store.Add(args[0], transport)
			if err != nil {
				return err
			}
			line, err := joinStringFor(f.keys(), store, r, port, transport, service)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out)
			fmt.Fprintf(out, "  added the room %q, reachable over %s.\n", r.Name, r.Transport)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "  On that machine, paste:")
			fmt.Fprintln(out)
			fmt.Fprintln(out, "      atrium2 join "+line)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "  Good once, for an hour, and for that name only. This is the only")
			fmt.Fprintln(out, "  time it is shown.")
			fmt.Fprintln(out)
			return nil
		},
	}
	f.bind(c)
	c.Flags().StringVar(&port, "link", ":7801", "where rooms dial in")
	c.Flags().StringVar(&transport, "transport", "direct",
		"how this room reaches the hub: direct, ziti or zrok")
	c.Flags().StringVar(&service, "service", "atrium-hub", "the ziti service, with --transport ziti")
	return c
}

// roomTokenCmd replaces a room's join string with a fresh one.
//
// NOT "SHOW ME THE OLD ONE", because the hub cannot: it holds a hash, the way a
// password store does. Regenerating is the honest version of that, and it also
// means a string that leaked stops working the moment somebody notices.
func roomTokenCmd() *cobra.Command {
	var f hubStoreFlags
	var port, service string
	c := &cobra.Command{
		Use:   "token <name>",
		Short: "Mint a fresh join string for a room, retiring the old one",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := f.open()
			if err != nil {
				return err
			}
			defer store.Close()

			r, err := store.ByName(args[0])
			if err != nil {
				return knownRooms(store, args[0], err)
			}
			line, err := joinStringFor(f.keys(), store, r, port, r.Transport, service)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), line)
			return nil
		},
	}
	f.bind(c)
	c.Flags().StringVar(&port, "link", ":7801", "where rooms dial in")
	c.Flags().StringVar(&service, "service", "atrium-hub", "the ziti service, with --transport ziti")
	return c
}

// joinStringFor mints the secret and wraps it up for the transport.
//
// The two halves are deliberately apart. The store decides what a credential is
// and which room it belongs to; the transport decides what else has to travel
// beside it. Neither knows the other's business, which is what lets a fourth
// transport arrive without touching the store.
//
// NOT THROUGH `openHub`, which is the same assembly the running hub does and is
// the wrong thing to call here: opening the zrok side RESERVES A SHARE, so a
// command that only meant to print a line would stand up a second way in and
// then tear it down. What that costs is worth avoiding for a `token` command
// somebody might run twice by accident.
func joinStringFor(keys link.Keys, store *hubstore.Store, r *hubstore.Room,
	port, transport, service string) (string, error) {
	if strings.TrimSpace(transport) == "" {
		transport = r.Transport
	}
	switch transport {
	case hubstore.TransportDirect:
		if _, err := keys.Fingerprint(); err != nil {
			return "", errors.New("this machine is not a hub yet. run `atrium2 hub` once " +
				"first, so it can make itself a certificate authority")
		}
		secret, err := store.Mint(r.ID)
		if err != nil {
			return "", err
		}
		return keys.MintToken(advertised(port), r.Name, secret)

	case hubstore.TransportZiti:
		// NO SECRET, because there is nothing for one to do: a policy decided
		// who may dial this service before atrium existed. The name still
		// travels, because the transport cannot supply it.
		return link.MintOverlayToken("ziti", r.Name, service, "")

	case hubstore.TransportZrok:
		// The share token belongs to the hub that reserved it, and this
		// command is not that process. Refused with the reason rather than
		// reserving a second share nobody asked for.
		return "", errors.New("a zrok join string carries the share the running hub " +
			"reserved, so it has to come from that hub rather than from here")
	}
	return "", fmt.Errorf("no transport called %q", transport)
}

func roomListCmd() *cobra.Command {
	var f hubStoreFlags
	c := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "Every room this hub knows about, connected or not",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := f.open()
			if err != nil {
				return err
			}
			defer store.Close()

			rooms, err := store.Rooms()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(rooms) == 0 {
				fmt.Fprintln(out, "no rooms yet. `atrium2 hub room add <name>` makes one")
				return nil
			}
			for _, r := range rooms {
				cards, err := store.CardCount(r.ID)
				if err != nil {
					return err
				}
				// WHAT THE ROOM CALLS ITSELF, BESIDE WHAT IT IS CALLED. Never
				// instead of: the hub's name is the name and the machine's own
				// is observed. Showing both is the point, and choosing between
				// them would be a second source of truth for one fact.
				line := fmt.Sprintf("%-20s %-10s %s", r.Name, r.Transport, sinceHeard(r))
				if r.SelfName != "" && !strings.EqualFold(r.SelfName, r.Name) {
					line += "   (calls itself " + r.SelfName + ")"
				}
				// THE CACHE IS READ ONLY WHEN THE ROOM IS NOT ANSWERING.
				//
				// Not a rule about staleness, a rule about there being one
				// answer. A room that is here can be asked, and printing a
				// remembered number beside a live room is the third state
				// decision 15 exists to rule out: some of what you are reading
				// is current, some is remembered, and nothing says which.
				if cards > 0 && !r.LikelyAttached() {
					line += fmt.Sprintf("   [%d card(s) when last heard from]", cards)
				}
				if ok, until, err := store.Outstanding(r.ID); err == nil && ok {
					line += "   join string good until " +
						until.Local().Format("15:04")
				}
				fmt.Fprintln(out, line)
			}
			return nil
		},
	}
	f.bind(c)
	return c
}

// roomMarkCmd is decision 9's reversible half.
func roomMarkCmd() *cobra.Command {
	var f hubStoreFlags
	var undo bool
	c := &cobra.Command{
		Use:   "mark <name>",
		Short: "Mark a room for deletion, or take the mark back off",
		Long: "A room marked for deletion starts no new cards. Everything already running\n" +
			"continues and is worked out normally, and the room stays on the list\n" +
			"throughout, because a room mid-cleanup is a thing that still exists.\n\n" +
			"Nothing about marking destroys anything, which is what makes it safe to\n" +
			"press. `--undo` puts it straight back into ordinary service.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := f.open()
			if err != nil {
				return err
			}
			defer store.Close()

			r, err := store.ByName(args[0])
			if err != nil {
				return knownRooms(store, args[0], err)
			}
			if err := store.Mark(r.ID, !undo); err != nil {
				return err
			}
			if undo {
				fmt.Fprintf(cmd.OutOrStdout(), "%s is back in ordinary service\n", r.Name)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(),
					"%s is marked for deletion. it starts no new cards, and this can be undone\n",
					r.Name)
			}
			return nil
		},
	}
	f.bind(c)
	c.Flags().BoolVar(&undo, "undo", false, "take the mark back off")
	return c
}

// roomRemoveCmd is the destructive half, and it argues with you first.
func roomRemoveCmd() *cobra.Command {
	var f hubStoreFlags
	var force bool
	c := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove"},
		Short:   "Remove a room this hub is done with",
		Long: "Ordinarily a room is marked for deletion, its cards are cleared, and the\n" +
			"room itself confirms they are done before the row goes. That confirmation\n" +
			"is not something a command line can get, so this refuses a room that has\n" +
			"not been marked.\n\n" +
			"`--force` is for a machine that is never coming back: a dead laptop, a\n" +
			"wiped disk. IT REMOVES THE HUB'S RECORD AND NOTHING ELSE. If that machine\n" +
			"ever comes back it is still holding cards, directories and sessions this\n" +
			"hub has now forgotten about.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := f.open()
			if err != nil {
				return err
			}
			defer store.Close()

			r, err := store.ByName(args[0])
			if err != nil {
				return knownRooms(store, args[0], err)
			}
			cards, err := store.CardCount(r.ID)
			if err != nil {
				return err
			}
			// A CONNECTED ROOM IS NOT DELETED, and `--force` does not change
			// that. Forcing is for a machine that is never coming back, and one
			// that said hello ten seconds ago is not that. Refused ahead of the
			// ordinary checks so the message is about the right thing.
			if r.LikelyAttached() {
				return fmt.Errorf("%s was heard from at %s, so it is still attached. "+
					"stop it there first. forcing is for a machine that is never "+
					"coming back", r.Name, r.LastSeen.Local().Format("15:04:05"))
			}
			if !force {
				if !r.Marked() {
					return fmt.Errorf("%s is not marked for deletion. "+
						"`atrium2 hub room mark %s` first, clear its cards, and let it "+
						"finish. `--force` skips all of that and is for a machine that "+
						"is never coming back", r.Name, r.Name)
				}
				if cards > 0 {
					return fmt.Errorf("%s was last seen holding %d card(s). clear them on "+
						"that machine first: they are work, on a computer this hub does not "+
						"own, and forgetting them here does not stop them", r.Name, cards)
				}
			}
			why := "removed from the hub"
			if force {
				why = fmt.Sprintf("forced out with %d card(s) last seen on it", cards)
				if err := store.Force(r.ID, why); err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "%s is gone from this hub.\n", r.Name)
				fmt.Fprintln(out, "that machine still has whatever it had. nothing there was "+
					"stopped, cleaned up or told.")
				return nil
			}
			if err := store.Remove(r.ID, why); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is gone from this hub\n", r.Name)
			return nil
		},
	}
	f.bind(c)
	c.Flags().BoolVar(&force, "force", false,
		"remove a room that is never coming back, without its confirmation")
	return c
}

// knownRooms turns "no room by that name" into the list that would have worked.
//
// The same courtesy `atrium tell` pays an unknown handle. Somebody who typed a
// name wrong is one line away from the right one, and making them run another
// command to find it is a small rudeness that adds up.
func knownRooms(store *hubstore.Store, asked string, cause error) error {
	if !errors.Is(cause, hubstore.ErrNoSuchRoom) {
		return cause
	}
	rooms, err := store.Rooms()
	if err != nil || len(rooms) == 0 {
		return fmt.Errorf("this hub has no room called %q, and no rooms at all. "+
			"`atrium2 hub room add %s` makes one", asked, asked)
	}
	names := make([]string, 0, len(rooms))
	for _, r := range rooms {
		names = append(names, r.Name)
	}
	return fmt.Errorf("this hub has no room called %q. it has: %s",
		asked, strings.Join(names, ", "))
}
