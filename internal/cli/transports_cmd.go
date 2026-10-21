package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/spf13/cobra"
)

// The two halves of the transport order: the hub's, which every room is told,
// and a room's own, which wins on that machine. See docs/rnd/room-transport-order.md.

// roomTransportsCmd is `atrium rooms transports`, the hub's order.
//
// ON THE HUB'S MACHINE, like `rooms legacy`, because it changes how rooms reach
// the hub and the board can be reached over a share. It orders, it does not
// enable: the hub listens on what `--transport` says.
func roomTransportsCmd(prefix string) *cobra.Command {
	var f hubStoreFlags
	c := &cobra.Command{
		Use:   "transports [order]",
		Short: "The order rooms try this hub's transports in, such as direct,ziti",
		Long: "Every room is told, when it attaches, the transports this hub listens on in\n" +
			"this order, and writes them down. A room that cannot reach the hub on the first\n" +
			"tries the next, and moves back up when the first answers again.\n\n" +
			"With no argument this prints the order. Unset is ziti,direct,zrok. A transport\n" +
			"the hub is not listening on is skipped, so this never turns one on. A room can\n" +
			"override it on its own machine with `atrium room transports order`.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := f.open()
			if err != nil {
				return err
			}
			defer store.Close()
			out := cmd.OutOrStdout()
			if len(args) == 0 {
				v, err := store.RoomTransportOrder()
				if err != nil {
					return err
				}
				order, _ := link.ParseOrder(v)
				if len(order) == 0 {
					fmt.Fprintln(out, strings.Join(link.DefaultOrder, ",")+"   (the default)")
					return nil
				}
				fmt.Fprintln(out, strings.Join(order, ","))
				return nil
			}
			order, err := link.ParseOrder(args[0])
			if err != nil {
				return err
			}
			if len(order) == 0 {
				return errors.New("name at least one transport: direct, ziti or zrok")
			}
			if err := store.SetRoomTransportOrder(strings.Join(order, ",")); err != nil {
				return err
			}
			f.nudge()
			fmt.Fprintln(out, strings.Join(order, ",")+"   rooms are told at their next attach")
			return nil
		},
	}
	f.bind(c, prefix)
	f.bindBoard(c, prefix)
	return c
}

// roomTransportsLocalCmd is `atrium room transports`, this room's own settings.
//
// It writes `transports.json` in the room's key directory and nothing else, so it
// works on a stopped room and on a room that cannot reach its hub, which is when
// it is needed. A running room reads it at its next start.
func roomTransportsLocalCmd() *cobra.Command {
	var dir string
	keysOf := func() link.Keys { return link.Keys{Dir: orDefault(dir, roomDir())} }

	c := &cobra.Command{
		Use:   "transports",
		Short: "The transports this room tries to reach its hub on, best first",
		Long: "Prints the ladder this room climbs: every way it can reach its hub, best first,\n" +
			"and where each came from (the join, the hub, or this machine). The settings are\n" +
			"in transports.json beside room.json. A running room reads them at its next start.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys := keysOf()
			saved, err := keys.Joined()
			if err != nil {
				return err
			}
			tr, err := keys.Transports()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			paths, skipped := link.Candidates(saved, tr)
			for i, p := range paths {
				fmt.Fprintf(out, "%d. %s\n", i+1, describePath(p))
			}
			for _, s := range skipped {
				fmt.Fprintln(out, "   not a rung: "+s)
			}
			who := "the hub's"
			if len(tr.Order) > 0 {
				who = "this room's own (" + strings.Join(tr.Order, ",") + ")"
			} else if len(tr.Hub) == 0 {
				who = "the default, nothing heard from the hub yet"
			}
			fmt.Fprintln(out, "order: "+who)
			fmt.Fprintln(out, "settings: "+keys.TransportsPath())
			return nil
		},
	}

	var clear bool
	order := &cobra.Command{
		Use:   "order <direct,ziti,...>",
		Short: "Set this room's own order, or --clear to follow the hub's",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := keysOf()
			tr, err := keys.Transports()
			if err != nil {
				return err
			}
			if clear {
				tr.Order = nil
			} else {
				if len(args) == 0 {
					return errors.New("name the order, such as ziti,direct, or pass --clear")
				}
				if tr.Order, err = link.ParseOrder(args[0]); err != nil {
					return err
				}
			}
			if err := keys.SaveTransports(tr); err != nil {
				return err
			}
			if len(tr.Order) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "this room follows the hub's order")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "this room tries "+strings.Join(tr.Order, ", then "))
			}
			return nil
		},
	}
	order.Flags().BoolVar(&clear, "clear", false, "drop this room's own order and follow the hub's")

	var identity, service string
	ziti := &cobra.Command{
		Use:   "ziti --identity <identity.json or jwt>",
		Short: "Give this room an OpenZiti identity to reach its hub with",
		Long: "An enrolled identity .json is used where it is. Anything else is a JWT, given\n" +
			"or as a path to one, and is enrolled into an identity kept in this room's key\n" +
			"directory. --service is the service the hub binds, default atrium-hub.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys := keysOf()
			tr, err := keys.Transports()
			if err != nil {
				return err
			}
			p := tr.Local("ziti")
			material := strings.TrimSpace(identity)
			switch {
			case material == "":
				return errors.New("--identity is an enrolled .json or a JWT to enroll")
			case isIdentityFile(material):
				abs, err := filepath.Abs(material)
				if err != nil {
					return err
				}
				if _, err := os.Stat(abs); err != nil {
					return fmt.Errorf("no identity at %s: %w", abs, err)
				}
				p.Identity = filepath.ToSlash(abs)
			default:
				if raw, err := os.ReadFile(material); err == nil {
					material = strings.TrimSpace(string(raw))
				}
				name := "room"
				if saved, err := keys.Joined(); err == nil && saved.Room != "" {
					name = saved.Room
				}
				path, out, err := daemon.EnrollZitiInto(material, name, filepath.Join(keys.Dir, "identities"))
				if err != nil {
					if strings.TrimSpace(out) != "" {
						return fmt.Errorf("could not enroll the ziti identity: %w\n%s", err, out)
					}
					return fmt.Errorf("could not enroll the ziti identity: %w", err)
				}
				p.Identity = filepath.ToSlash(path)
			}
			if s := strings.TrimSpace(service); s != "" {
				p.Service = s
			}
			tr.SetLocal(p)
			if err := keys.SaveTransports(tr); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "ziti: "+describePath(p))
			return nil
		},
	}
	ziti.Flags().StringVar(&identity, "identity", "", "an enrolled identity .json, or a JWT or a path to one")
	ziti.Flags().StringVar(&service, "service", "", "the ziti service the hub binds, default "+link.DefaultZitiService)

	var token string
	zrok := &cobra.Command{
		Use:   "zrok --token <share token>",
		Short: "Give this room the private zrok share to reach its hub with",
		Long: "The token is the one the hub printed when it started over zrok, and is what a zrok join string\n" +
			"carries. This machine needs `zrok enable` run as well. The token is kept in transports.json.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys := keysOf()
			tr, err := keys.Transports()
			if err != nil {
				return err
			}
			t := strings.TrimSpace(token)
			if t == "" {
				return errors.New("--token is the hub's private zrok share token")
			}
			p := tr.Local("zrok")
			p.Share = t
			tr.SetLocal(p)
			if err := keys.SaveTransports(tr); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "zrok: "+describePath(p))
			return nil
		},
	}
	zrok.Flags().StringVar(&token, "token", "", "the hub's private zrok share token")

	var addr string
	direct := &cobra.Command{
		Use:   "direct --addr <host:port>",
		Short: "Reach the hub by direct mutual TLS at this address first, before the joined address and the hub's",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys := keysOf()
			tr, err := keys.Transports()
			if err != nil {
				return err
			}
			a := hostPort(addr)
			if a == "" {
				return errors.New("--addr is the hub's host:port")
			}
			p := tr.Local("direct")
			p.Addr = a
			tr.SetLocal(p)
			if err := keys.SaveTransports(tr); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "direct: "+a)
			return nil
		},
	}
	direct.Flags().StringVar(&addr, "addr", "", "the hub's host:port")

	c.AddCommand(order, ziti, zrok, direct)
	for _, x := range []*cobra.Command{c, order, ziti, zrok, direct} {
		x.Flags().StringVar(&dir, "dir", "", "where this room keeps its certificate")
	}
	return c
}

// describePath says one rung for a person.
func describePath(p link.Path) string {
	s := p.Transport
	switch p.Transport {
	case "direct":
		s += " " + p.Addr
	case "ziti":
		svc := p.Service
		if svc == "" {
			svc = link.DefaultZitiService
		}
		s += " service " + svc
		if p.Identity != "" {
			s += " as " + p.Identity
		}
	case "zrok":
		s += " private share"
	}
	if p.From != "" {
		s += "   (" + p.From + ")"
	}
	return s
}
