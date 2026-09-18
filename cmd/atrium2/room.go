package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/spf13/cobra"
)

// The room: everything that owns something.
//
// A room is an ordinary atrium daemon. It has the store, the pseudo terminals
// and the agents, and it serves its board on loopback exactly as it always did.
// The only new thing is that it also dials a hub and serves that SAME handler
// down the connection, so the board can be served from somewhere else by a
// process that can be restarted freely.

func joinCmd() *cobra.Command {
	var (
		dir      string
		db       string
		human    string
		agent    string
		identity string
	)
	c := &cobra.Command{
		Use:   "join <join string>",
		Short: "Run the agents here and attach them to a hub",
		Long: "Takes the line `atrium2 hub room add` printed, and does everything else.\n\n" +
			"It makes this room a key, gets a certificate from that hub, saves both, and\n" +
			"then runs. The private key never leaves this machine: the hub signs a request\n" +
			"and never sees the key that made it.\n\n" +
			"THE HUB DECIDES WHAT THIS ROOM IS CALLED. The name was chosen when the room\n" +
			"was added there and it travels in the join string, so there is nothing to\n" +
			"pick here. This machine's own name is still reported, and shown beside it.\n\n" +
			"After the first time, `atrium2 room` runs with what was saved and needs no\n" +
			"arguments at all.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			j, err := link.ParseToken(args[0])
			if err != nil {
				return err
			}
			keys := link.Keys{Dir: orDefault(dir, roomDir())}

			// WHAT THIS MACHINE CALLS ITSELF, WHICH IS NOT WHAT IT IS CALLED.
			// Sent so the hub can show the two side by side, and it decides
			// nothing: this is `docs/architecture-v2.md`'s observed-versus-
			// overrides rule, where the hub's name is the override.
			self := defaultRoomName()
			name := j.Name

			// ONLY THE DIRECT TRANSPORT HAS ANYTHING TO ENROL. Under ziti and
			// zrok the network already decided who may connect, so joining is
			// writing down where the hub is and starting.
			if j.Transport == "direct" {
				d := link.Direct{Addr: j.Addr, Keys: keys, Pin: j.Pin}
				ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
				// THE NAME COMES BACK OUT OF THE SIGNED CERTIFICATE rather than
				// out of the string that was pasted. They are the same name
				// unless somebody edited the string, and when they differ the
				// hub's signature is the one that decides where requests go.
				name, err = d.Enrol(ctx, self, j.Secret)
				cancel()
				if err != nil {
					return err
				}
			} else if err := keys.SaveOverlayRoom(j, name, identity); err != nil {
				return err
			}
			fmt.Println()
			fmt.Println("  joined over " + j.Transport + " as \"" + name + "\".")
			if !strings.EqualFold(name, self) {
				fmt.Println("  this machine calls itself \"" + self + "\", which the hub shows beside it.")
			}
			fmt.Println("  starting the room. `atrium2 room` is all it takes from now on.")
			fmt.Println()
			return runRoom(keys, db, human, agent)
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "where this room keeps its certificate")
	c.Flags().StringVar(&db, "db", "", "the room's database")
	c.Flags().StringVar(&human, "http", "127.0.0.1:7810", "the room's own board, for when the hub is down")
	c.Flags().StringVar(&agent, "agent", "127.0.0.1:7811", "where this room's agents report")
	c.Flags().StringVar(&identity, "identity", "",
		"a ziti identity file, when the join string is for a ziti service")
	acceptUpgradeFlag(c)
	return c
}

// acceptUpgradeFlag is the one decision that lets a hub's build reach a room.
//
// The wording is deliberate. It is not "auto update": the room fetches, hashes
// and checks before anything is swapped, and a hub can never make it happen.
func acceptUpgradeFlag(c *cobra.Command) {
	c.Flags().BoolVar(&acceptUpgrades, "accept-upgrades", false,
		"take a newer atrium2 from the hub when it has one, verify it, and restart")
}

func roomCmd() *cobra.Command {
	var dir, db, human, agent string
	c := &cobra.Command{
		Use:   "room",
		Short: "Run the agents here, attached to the hub this machine already joined",
		Long: "Runs a full atrium: the database, the terminals and the agents.\n\n" +
			"It also serves its own board on loopback. That is not a leftover, it is the\n" +
			"escape hatch: when the hub is down or you broke it, the room is still a\n" +
			"working atrium at its own address and your agents never noticed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRoom(link.Keys{Dir: orDefault(dir, roomDir())}, db, human, agent)
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "where this room keeps its certificate")
	c.Flags().StringVar(&db, "db", "", "the room's database")
	c.Flags().StringVar(&human, "http", "127.0.0.1:7810", "the room's own board, for when the hub is down")
	c.Flags().StringVar(&agent, "agent", "127.0.0.1:7811", "where this room's agents report")
	acceptUpgradeFlag(c)
	return c
}


// askToStop winds this room down the way ctrl-c does.
//
// A package variable because the thing that needs it is the upgrade install,
// which runs deep inside the link and has no business holding the room's
// context. Nil before a room is running, which is when nothing can ask.
var stopRoomNow func()

func askToStop() {
	if stopRoomNow != nil {
		stopRoomNow()
	}
}

// runRoom starts the daemon and attaches it to the hub.
func runRoom(keys link.Keys, db, human, agent string) error {
	saved, err := keys.Joined()
	if err != nil {
		return err
	}
	dial, err := roomDialer(link.Join{
		Transport: saved.Transport, Addr: saved.Hub,
		Service: saved.Service, ShareToken: saved.Share,
	}, keys, saved.Identity)
	if err != nil {
		return err
	}
	if strings.TrimSpace(db) == "" {
		db = defaultRoomDB()
	}

	// BEFORE ANYTHING OPENS. This is the line that stops a room hijacking the
	// hooks of an atrium already running as the same user, which on this
	// machine is the difference between a demo and a broken afternoon. See
	// `LocationPath` in internal/daemon.
	if os.Getenv("ATRIUM_LOCATION") == "" {
		if err := os.Setenv("ATRIUM_LOCATION", roomLocation()); err != nil {
			return err
		}
	}
	// And the shared copy too, which is a second file written for callers
	// running as another account. A room must not publish itself as the
	// machine's atrium.
	if os.Getenv("ATRIUM_SHARED_LOCATION") == "" {
		if err := os.Setenv("ATRIUM_SHARED_LOCATION", "-"); err != nil {
			return err
		}
	}

	d, err := daemon.New(daemon.Options{
		HumanAddr: human,
		AgentAddr: agent,
		DBPath:    db,
		// THE HUB-DECIDED NAME, the same one the link attaches under below, so
		// every session this room launches carries ATRIUM_ROOM and its HTTP
		// control MCP registration can name this room to the hub.
		Room: saved.Room,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// So an installed upgrade can ask for the same wind-down ctrl-c gets,
	// rather than inventing a second way to stop. See `askToStop`.
	stopRoomNow = stop

	// THE LINK IS BEST EFFORT AND THE DAEMON IS NOT.
	//
	// A hub that is unreachable, misconfigured, or simply not started yet must
	// never stop a room from running. That is the same posture every other
	// outward-facing thing in atrium takes: a hook that cannot reach the daemon
	// fails open, a source that fails is reported on its own row. A room whose
	// hub is down is a room with nobody watching it, which is exactly what it
	// was before anybody built a hub.
	room := &link.Room{
		Name:    saved.Room,
		Dial:    dial,
		Handler: d.BoardHandler(),
		Version: version,
		Host:    hostname(),
		// WHETHER THIS ROOM WILL TAKE A BUILD ITS HUB IS RUNNING, which is the
		// operator's decision and is off unless they made it. A hub can always
		// SAY what it has; nothing happens here unless this is on. See
		// `internal/link/upgrade.go`.
		Upgrades: &link.Upgrades{
			Accept:  acceptUpgrades,
			Version: version,
			// WHAT THIS ROOM IS RUNNING, so it cannot be talked into
			// installing itself. See `Upgrades.SHA256`.
			SHA256:  selfSHA(version),
			Dir:     selfDir(),
			Install: installUpgrade,
		},
	}
	go func() {
		if err := room.Run(ctx); err != nil {
			log.Printf("[link] gave up on the hub: %v", err)
		}
	}()

	fmt.Println()
	fmt.Println("  room     " + saved.Room)
	fmt.Println("  hub      " + dial.Describe())
	fmt.Println("  database " + db)
	fmt.Println("  own board http://" + human + "   (for when the hub is down)")
	fmt.Println()

	return d.Run(ctx)
}

// defaultRoomName is the machine's name, because that is what somebody would
// have typed.
func defaultRoomName() string {
	if h := hostname(); h != "" {
		return h
	}
	return "room"
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// defaultRoomDB is deliberately NOT the database `atrium` uses.
//
// The operator was explicit: a clean slate, and no care about the agents in the
// old one until there is something worth migrating. Pointing at the same file
// would also mean two daemons with one sqlite database, which is the one thing
// the store is not built for.
// roomLocation is the room's own address file, beside its database rather than
// in the place the machine's atrium already owns.
func roomLocation() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "atrium2", "room", "daemon.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".atrium2", "room", "daemon.json")
}

func defaultRoomDB() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "atrium2", "room", "atrium2.db")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".atrium2", "room", "atrium2.db")
}
