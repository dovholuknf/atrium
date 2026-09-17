package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
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
		name     string
		dir      string
		db       string
		human    string
		agent    string
		identity string
	)
	c := &cobra.Command{
		Use:   "join <join string>",
		Short: "Run the agents here and attach them to a hub",
		Long: "Takes the line `atrium2 hub` printed, and does everything else.\n\n" +
			"It makes this room a key, gets a certificate from that hub, saves both, and\n" +
			"then runs. The private key never leaves this machine: the hub signs a request\n" +
			"and never sees the key that made it.\n\n" +
			"After the first time, `atrium2 room` runs with what was saved and needs no\n" +
			"arguments at all.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			j, err := link.ParseToken(args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(name) == "" {
				name = defaultRoomName()
			}
			keys := link.Keys{Dir: orDefault(dir, roomDir())}

			// ONLY THE DIRECT TRANSPORT HAS ANYTHING TO ENROL. Under ziti and
			// zrok the network already decided who may connect, so joining is
			// writing down where the hub is and starting.
			if j.Transport == "direct" {
				d := link.Direct{Addr: j.Addr, Keys: keys, Pin: j.Pin}
				ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
				err = d.Enrol(ctx, name, j.Secret)
				cancel()
				if err != nil {
					return err
				}
			} else if err := keys.SaveOverlayRoom(j, name, identity); err != nil {
				return err
			}
			fmt.Println()
			fmt.Println("  joined over " + j.Transport + " as \"" + name + "\".")
			fmt.Println("  starting the room. `atrium2 room` is all it takes from now on.")
			fmt.Println()
			return runRoom(keys, db, human, agent)
		},
	}
	c.Flags().StringVar(&name, "name", "", "what to call this room (default: this machine's name)")
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

// hubAsRoom runs a room inside the hub's own process.
//
// ── what this is for ─────────────────────────────────────
//
// The machine the hub is on is usually a machine you also work on, and telling
// somebody to start a second process in a second terminal to use the computer
// in front of them is a silly answer to a question they should not have had to
// ask.
//
// ── what it costs, which is why it is off by default ─────
//
// A hub that holds a database is a hub whose restart is no longer free. That
// is the one property the split exists to buy, and this gives it up for the
// hub's own machine. Rooms attached from elsewhere are untouched either way:
// they carry their own database and their own terminals, and a hub restart is
// still just a reconnect to them.
//
// ── how it attaches ──────────────────────────────────────
//
// Over `link.InProc`, which is a transport like any other: a listener and a
// dialer, joined by a pipe. The room enrols, heartbeats and pools connections
// exactly as a machine across the world does, and appears in `Rooms()` beside
// them. Nothing above this knows the difference, which is what stops the two
// paths drifting apart.
// ── the switch ───────────────────────────────────────────

// ownRoom is the hub's own room, and whether it is running.
//
// A TYPE RATHER THAN A FLAG, because this is turned on and off from the board
// while the hub keeps running. Stopping it cancels its context, which stops
// the daemon and drops its link, and the hub goes back to holding nothing.
type ownRoom struct {
	parent context.Context
	hub    *link.Hub
	dir    string

	name, db, agent string

	mu     sync.Mutex
	cancel context.CancelFunc
}

func (o *ownRoom) Name() string { return o.name }

func (o *ownRoom) On() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cancel != nil
}

// Set starts or stops the room, and writes down which, so a hub restart comes
// back the way it was left rather than the way it was first started.
func (o *ownRoom) Set(on bool) error {
	o.mu.Lock()
	already := o.cancel != nil
	o.mu.Unlock()
	if on == already {
		return o.remember(on)
	}
	if !on {
		o.mu.Lock()
		stop := o.cancel
		o.cancel = nil
		o.mu.Unlock()
		if stop != nil {
			stop()
		}
		log.Printf("[hub] stopped being a room. its agents keep running, unsupervised, " +
			"until something ends them")
		return o.remember(false)
	}
	ctx, cancel := context.WithCancel(o.parent)
	if err := hubAsRoom(ctx, o.hub, o.name, o.db, o.agent); err != nil {
		cancel()
		return err
	}
	o.mu.Lock()
	o.cancel = cancel
	o.mu.Unlock()
	return o.remember(true)
}

// remember writes the answer beside the hub's certificates.
//
// NOT IN A DATABASE, because the thing being remembered is whether to open one.
func (o *ownRoom) remember(on bool) error {
	if strings.TrimSpace(o.dir) == "" {
		return nil
	}
	if err := os.MkdirAll(o.dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(map[string]bool{"on": on})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(o.dir, "room.json"), raw, 0o600)
}

// wasOn reads what was written last time. False for a hub that has never been
// one, which is the default and the point.
func wasOn(dir string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "room.json"))
	if err != nil {
		return false
	}
	var body struct {
		On bool `json:"on"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return false
	}
	return body.On
}

func hubAsRoom(ctx context.Context, h *link.Hub, name, db, agent string) error {
	if strings.TrimSpace(name) == "" {
		name = defaultRoomName()
	}
	if strings.TrimSpace(db) == "" {
		db = defaultRoomDB()
	}
	// The same two lines a separate room runs, and for the same reason: a room
	// must not publish itself as the machine's atrium and hijack the hooks of
	// one already running. See `runRoom`.
	if os.Getenv("ATRIUM_LOCATION") == "" {
		if err := os.Setenv("ATRIUM_LOCATION", roomLocation()); err != nil {
			return err
		}
	}
	if os.Getenv("ATRIUM_SHARED_LOCATION") == "" {
		if err := os.Setenv("ATRIUM_SHARED_LOCATION", "-"); err != nil {
			return err
		}
	}

	d, err := daemon.New(daemon.Options{
		// NO BOARD OF ITS OWN. A separate room serves one on loopback for when
		// the hub is down, which cannot happen to this one: they are the same
		// process, so if the hub is down so is this.
		HumanAddr: "-",
		AgentAddr: agent,
		DBPath:    db,
	})
	if err != nil {
		return err
	}

	pipe := &link.InProc{}
	go func() {
		<-ctx.Done()
		_ = pipe.Close()
	}()
	go func() {
		if err := h.Serve(ctx, pipe.Listen()); err != nil && ctx.Err() == nil {
			log.Printf("[hub] its own room stopped listening: %v", err)
		}
	}()
	room := &link.Room{
		Name: name, Dial: pipe.Dialer(), Handler: d.BoardHandler(),
		Version: version, Host: hostname(),
	}
	go func() {
		if err := room.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[hub] its own room gave up: %v", err)
		}
	}()
	go func() {
		if err := d.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[hub] its own room's daemon stopped: %v", err)
		}
	}()

	log.Printf("[hub] also a room, called %q, agents on %s, state in %s", name, agent, db)
	return nil
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
