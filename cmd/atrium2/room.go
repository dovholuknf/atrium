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
		isolated bool
		flags    joinFlags
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
			// THE FLAGS DECIDE THE TRANSPORT, layered on the token that names the
			// room and pins the hub. With no flag the token decides, exactly as
			// before. See joinflags.go and the design's "The room link".
			flags.identity = identity
			j, ident, jwt, err := flags.resolve(j)
			if err != nil {
				return err
			}
			identity = ident
			keys := link.Keys{Dir: orDefault(dir, roomDir())}

			if jwt != "" {
				// ENROLLING A JWT IN PLACE, the design's room-link ziti path. The
				// token is turned into an identity `.json` kept on THIS room's own
				// disk, beside its certificate, and that file is what every later
				// dial uses. This is the same shell-out the board-exposure path
				// makes (internal/daemon.EnrollZiti); the identity name is the
				// room's own so a machine with more than one is told them apart.
				// See docs/ziti-zrok-flow-design.md, "atrium2 join enrolling a JWT
				// in place".
				dir := filepath.Join(keys.Dir, "identities")
				path, out, err := daemon.EnrollZitiInto(jwt, j.Name, dir)
				if err != nil {
					if strings.TrimSpace(out) != "" {
						return fmt.Errorf("could not enroll the ziti identity: %w\n%s", err, out)
					}
					return fmt.Errorf("could not enroll the ziti identity: %w", err)
				}
				fmt.Println("  enrolled a ziti identity at " + path)
				identity = path
			}

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
			return runRoom(keys, db, human, agent, 0, isolated)
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "where this room keeps its certificate")
	c.Flags().StringVar(&db, "db", "", "the room's database")
	c.Flags().StringVar(&human, "http", "127.0.0.1:7810", "the room's own board, for when the hub is down")
	c.Flags().StringVar(&agent, "agent", "127.0.0.1:7811", "where this room's agents report")
	c.Flags().StringVar(&identity, "identity", "",
		"a ziti identity file, when the join string is for a ziti service")
	// The transport flags: how to reach the hub and what to prove the right with,
	// on top of the token that names the room. Exactly one may be given; none
	// falls back to the transport the token itself encodes. See joinflags.go.
	c.Flags().StringVar(&flags.mtls, "mtls", "",
		"reach the hub by direct mutual TLS at this url or host:port (the LAN and no-overlay path)")
	c.Flags().StringVar(&flags.zrokPrivate, "zrok-private", "",
		"reach the hub over a private zrok share, with the share's access token")
	c.Flags().StringVar(&flags.openziti, "openziti", "",
		"reach the hub over an OpenZiti service, with an enrolled identity .json (or a jwt to enroll)")
	c.Flags().StringVar(&flags.service, "service", "",
		"the ziti service to dial with --openziti, default \"atrium\"")
	isolatedFlag(c, &isolated)
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

// isolatedFlag is how a SECOND room on a machine keeps its hands off the first
// one's hooks.
//
// The default is right for the normal case, which is one room per machine: it
// publishes itself at the fixed shared path every hook, CLI call and control MCP
// looks in without being told where the room's dir is. A second room started the
// same way overwrites that pointer, and from then on the first room's hooks
// arrive at the throwaway. `--isolated` says this is that second room: keep the
// address in a private file beside `--dir` and never touch the shared one. See
// `roomLocationEnv` and internal/daemon.LocationPath.
func isolatedFlag(c *cobra.Command, isolated *bool) {
	c.Flags().BoolVar(isolated, "isolated", false,
		"keep this room's address in a private file beside --dir, for a throwaway or "+
			"second room that must not take over the machine's hooks")
}

func roomCmd() *cobra.Command {
	var dir, db, human, agent string
	var isolated bool
	c := &cobra.Command{
		Use:   "room",
		Short: "Run the agents here, attached to the hub this machine already joined",
		Long: "Runs a full atrium: the database, the terminals and the agents.\n\n" +
			"It also serves its own board on loopback. That is not a leftover, it is the\n" +
			"escape hatch: when the hub is down or you broke it, the room is still a\n" +
			"working atrium at its own address and your agents never noticed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRoom(link.Keys{Dir: orDefault(dir, roomDir())}, db, human, agent, restartAfter, isolated)
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "where this room keeps its certificate")
	c.Flags().StringVar(&db, "db", "", "the room's database")
	c.Flags().StringVar(&human, "http", "127.0.0.1:7810", "the room's own board, for when the hub is down")
	c.Flags().StringVar(&agent, "agent", "127.0.0.1:7811", "where this room's agents report")
	isolatedFlag(c, &isolated)
	// Hidden: how a hub-triggered restart re-invokes this room detached. It waits
	// for the old process to release its ports, then starts as usual. Running it
	// by hand just adds a pointless pause. See restart.go.
	c.Flags().DurationVar(&restartAfter, "restart-after", 0, "internal: wait this long for the old room to exit first")
	_ = c.Flags().MarkHidden("restart-after")
	acceptUpgradeFlag(c)
	return c
}

// restartAfter is set only by the detached restarter a hub-triggered restart
// spawns, so a fresh room waits for the old one's ports to free before binding.
var restartAfter time.Duration

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
func runRoom(keys link.Keys, db, human, agent string, restartAfter time.Duration, isolated bool) error {
	// A HUB-TRIGGERED RESTART GOT HERE DETACHED, and the old room may still hold
	// the ports. Wait for it to let go before anything tries to bind, or the new
	// room fails to listen and exits, which looks like the restart doing nothing.
	if restartAfter > 0 {
		log.Printf("[atrium] restarting: waiting for the previous room to release %s", human)
		waitForRoomRestart(restartAfter, human)
	}

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

	// BEFORE ANYTHING OPENS. These are the lines that stop a room hijacking the
	// hooks of an atrium already running as the same user, which on this
	// machine is the difference between a demo and a broken afternoon. See
	// `LocationPath` in internal/daemon and `roomLocationEnv` below.
	location, shared := roomLocationEnv(isolated, keys.Dir)
	if os.Getenv("ATRIUM_LOCATION") == "" {
		if err := os.Setenv("ATRIUM_LOCATION", location); err != nil {
			return err
		}
	}
	// And the shared copy too, which is a second file written for callers
	// running as another account. A room must not publish itself as the
	// machine's atrium.
	if os.Getenv("ATRIUM_SHARED_LOCATION") == "" {
		if err := os.Setenv("ATRIUM_SHARED_LOCATION", shared); err != nil {
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
		// WHAT THIS ROOM DOES WHEN ITS HUB ASKS IT TO RESTART. Park the other
		// agents, spawn a detached restarter that outlives this process, and wind
		// down. The hub only forwards the ask. See restart.go.
		OnRestart: onHubRestart("http://"+human, human, agent, db, stop),
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
// roomLocationEnv decides the two values that keep a room's hooks pointed at
// itself: where it writes its own address, and whether it also publishes a
// shared copy.
//
// A room never writes the shared copy, isolated or not: the shared file is the
// machine's answer to "where is atrium", and a room is not the machine's atrium.
// So the shared value is always `-`, which internal/daemon.SharedLocationPath
// reads as "write none".
//
// The address file is where the difference lives. The normal room uses the
// FIXED shared path so hooks, the CLI and the control MCP find it without being
// told its dir. That is right for one room per machine and wrong for two: a
// second room started the same way overwrites the pointer and steals the first
// room's hooks. `--isolated` says this is that second, throwaway room, so its
// address goes in a PRIVATE file beside its own `--dir` and the first room's
// pointer is left alone.
func roomLocationEnv(isolated bool, dir string) (location, shared string) {
	if isolated {
		return filepath.Join(dir, "daemon.json"), "-"
	}
	return roomLocation(), "-"
}

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
