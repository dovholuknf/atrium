package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/spf13/cobra"
)

// The hub: serve the board, hold nothing, hand everything else to a room.

func hubCmd() *cobra.Command {
	var (
		board     string
		port      string
		dir       string
		db        string
		transport string
		service   string
		files     string
		open      bool

		// Binaries to offer rooms, one per platform. Empty means this hub can
		// only offer what it is running, which is no use to a room on another
		// kind of machine. See `builds.go`.
		buildDir string

		// The hub being a room too. Off unless asked for: see `--room`.
		asRoom    bool
		roomName  string
		roomDB    string
		roomAgent string
	)
	c := &cobra.Command{
		Use:   "hub",
		Short: "Serve the board. Holds nothing and can be restarted at will",
		Long: "Serves the board on a port your browser opens, and listens on a second port\n" +
			"for rooms to dial in to.\n\n" +
			"THE HUB HOLDS NO WORK. No sessions, no terminals, no agent processes, and\n" +
			"no authority over any of them. Everything the board shows about a connected\n" +
			"room comes from that room, live. That is what makes it safe to restart while\n" +
			"somebody's agent is mid-sentence.\n\n" +
			"It does keep a small store of its own: which rooms exist, what they are\n" +
			"called, their join strings, and a cache of what each one last said so an\n" +
			"offline room still shows what was there. None of that is work, and none of\n" +
			"it is authoritative while a room is connected.\n\n" +
			"On first run it makes itself a certificate authority and prints a join string.\n" +
			"Paste that into `atrium2 join` on the machine your agents are on.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys := link.Keys{Dir: orDefault(dir, hubDir())}

			// THE HUB'S OWN STORE, and opening it is the first thing that can
			// refuse to start.
			//
			// It holds no work: no sessions, no terminals, no agent processes.
			// It holds which rooms exist, what they are called, their join
			// secrets, which are on their way out, and a cache of what each one
			// last said. Nobody else can answer any of that, because the
			// question is whether a room exists at all.
			//
			// A failure here is tier one. A hub that keeps serving while it
			// cannot remember which rooms exist will mint a second room under a
			// name it has forgotten. That the work is safe on the rooms is not
			// a reason to stay up: it is the reason the halt costs little.
			store, err := hubstore.Open(orDefault(db, filepath.Join(keys.Dir, "hub.db")))
			if err != nil {
				return fmt.Errorf("the hub's store: %w", err)
			}
			defer store.Close()
			if store.Fresh() {
				// SAID LOUDLY, because a hub pointed at the wrong directory
				// looks exactly like every room having vanished, and the rooms
				// themselves would still be dialling in and being refused.
				log.Printf("[hub] made a new store at %s. no rooms are on it yet",
					orDefault(db, filepath.Join(keys.Dir, "hub.db")))
			}

			// SPENDING A SECRET IS WHAT SAYS WHICH ROOM THIS IS, and the store
			// is the only thing that can answer it. Handed to the transport
			// rather than reached for, so `internal/link` never learns the hub
			// has a database.
			side, err := openHub(transport, keys, port, service,
				func(secret string) (string, error) {
					r, err := store.Spend(secret)
					if err != nil {
						return "", err
					}
					return r.Name, nil
				})
			if err != nil {
				return err
			}
			defer side.release()

			ln, err := side.listen()
			if err != nil {
				return err
			}
			defer ln.Close()

			// WHAT A HALT DOES, and it is the daemon's posture one level up.
			//
			// The room listener closes and stays closed, so rooms see a refused
			// connection, which is the one failure their client already absorbs
			// silently: every one parks on the backoff it already has and burns
			// nothing. The board stays up to say what broke, because a hub that
			// vanishes explains nothing.
			//
			// The work is untouched. Every session is on a room, on a machine
			// this process does not own.
			store.OnHalt = func(cause error) {
				log.Printf("[hub] THE STORE HAS HALTED: %v", cause)
				log.Printf("[hub] rooms can no longer attach. the board stays up. " +
					"the agents on every room are unaffected and keep running")
				_ = ln.Close()
			}

			h := link.NewHub(link.Timings{})
			// WHAT THIS HUB CAN HAND OUT, so a room that asked to be told can
			// be told. Hashed once here rather than per attach: these are tens
			// of megabytes each and they cannot change under a running hub.
			//
			// One per platform, because a room is on another machine and a hub
			// that can only offer its own binary is no use to a fleet that is
			// not all the same. See `builds.go`.
			builds := hubBuilds(buildDir, version)
			h.Offers(builds...)
			if what := saysWhatItHas(builds); what != "" {
				log.Printf("[hub] builds on offer, for rooms that asked: %s", what)
			}
			h.Enrol = side.enrol
			// A ROOM IN THIS PROCESS IS NOT ASKED FOR PAPERS. Every other room
			// proves who it is with a certificate this hub signed, because its
			// connection crossed a network. That one did not leave the
			// process. See `link.IsInProc`.
			h.Authenticated = func(c net.Conn) bool {
				return link.IsInProc(c) || side.auth(c)
			}
			// A ROOM THIS HUB HAS NO RECORD OF DOES NOT ATTACH, even holding a
			// certificate this hub signed, because a certificate is not a
			// record: a room that was forced out still has its papers. The
			// same call writes down the observed half of what it says about
			// itself, which is the only moment the hub hears it.
			// WHAT THIS TRANSPORT ACTUALLY PROVES, said out loud at startup.
			//
			// Direct mTLS spends a secret bound to one room and hands back a
			// certificate carrying that name, so a room is what the hub signed.
			// An overlay proves somebody may connect and says nothing about
			// which room they are, so the name is asserted by the room and
			// checked only against the list. Nobody should have to read
			// `certs.go` to find that out.
			if transport != "" && transport != "direct" {
				log.Printf("[hub] rooms over %s prove they may connect, not which room "+
					"they are. a name here is taken on trust and checked against the "+
					"list, and binding one to an overlay identity is an open question",
					transport)
			}
			h.Attaching = func(name, host, ver string) error {
				r, err := store.ByName(name)
				if err != nil {
					return fmt.Errorf("this hub has no room called %q. "+
						"`atrium2 hub room add %s` on the hub makes one, and prints "+
						"the string to paste here", name, name)
				}
				return store.Seen(r.ID, host, ver)
			}
			// WHAT A ROOM SAYS IT IS HOLDING, TAKEN WHOLE. Anything the hub
			// was keeping for that room and is not in this is discarded,
			// because it is no longer there, and the discard is written down
			// rather than being silent. See `Announce` in internal/hubstore.
			h.Cached = func(name string, cards []link.CardState) error {
				r, err := store.ByName(name)
				if err != nil {
					return err
				}
				out := make([]hubstore.Card, 0, len(cards))
				for _, c := range cards {
					out = append(out, hubstore.Card{
						ID: c.ID, Status: c.Status, Payload: c.Payload,
					})
				}
				_, err = store.Announce(r.ID, out)
				return err
			}

			// The board this hub serves. From disk when told to, so the loop is
			// edit, save, reload, with no rebuild at all.
			assets, id, err := boardFrom(files)
			if err != nil {
				return err
			}
			proxy := link.NewProxy(h, assets, id, nil)
			// THE DURABLE LIST, which is a different question from what is
			// attached and gets a different endpoint for exactly that reason.
			proxy.SetInventory(inventory{store: store, hub: h})

			ctx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			go func() {
				if err := h.Serve(ctx, ln); err != nil && ctx.Err() == nil {
					log.Printf("[hub] the room listener stopped: %v", err)
				}
			}()

			// THE HUB AS A ROOM AS WELL, which is a switch rather than only a
			// flag: it is turned on and off from the board while the hub keeps
			// running, and what it was left as is what it comes back as.
			//
			// The flag still exists, and it only ever turns it ON. A hub
			// started without it that was left on last time stays on, because
			// the absence of a flag is not somebody asking for anything.
			own := &ownRoom{
				parent: ctx, hub: h, dir: keys.Dir, store: store,
				name: orDefault(roomName, defaultRoomName()),
				db:   orDefault(roomDB, defaultRoomDB()), agent: roomAgent,
			}
			proxy.SetOwnRoom(own)
			if asRoom || wasOn(keys.Dir) {
				if err := own.Set(true); err != nil {
					return err
				}
			}

			srv := &http.Server{
				Addr:    board,
				Handler: proxy,
				// NO WRITE TIMEOUT. The event stream and the terminal are both
				// meant to stay open for hours, and a write deadline would cut
				// them with nothing to show for it.
				ReadHeaderTimeout: 10 * time.Second,
			}
			boardLn, err := net.Listen("tcp", board)
			if err != nil {
				return fmt.Errorf("board listener: %w", err)
			}

			greet(store, side, board, id)

			go func() {
				<-ctx.Done()
				log.Printf("[hub] stopping. rooms will reconnect when it comes back")
				_ = srv.Close()
			}()
			if err := srv.Serve(boardLn); err != nil && ctx.Err() == nil {
				return err
			}
			return nil
		},
	}
	c.Flags().StringVar(&board, "addr", ":7800", "where the browser reaches the board")
	c.Flags().StringVar(&port, "link", ":7801", "where rooms dial in")
	c.Flags().StringVar(&dir, "dir", "", "where this hub keeps its certificates")
	c.Flags().StringVar(&db, "db", "",
		"the hub's own store: which rooms exist and what they last said (default: under --dir)")
	c.Flags().StringVar(&transport, "transport", "direct",
		"how rooms reach this hub: direct, ziti or zrok")
	c.Flags().StringVar(&service, "service", "atrium-hub", "the ziti service, with --transport ziti")
	c.Flags().StringVar(&zitiIdentity, "identity", "", "the ziti identity file, with --transport ziti")
	c.Flags().StringVar(&files, "board", "",
		"serve the board from this directory instead of the built-in copy")
	c.Flags().BoolVar(&open, "open", false, "print the address and nothing else")
	c.Flags().StringVar(&buildDir, "builds", "",
		"a directory of atrium2_<os>_<arch> binaries to offer rooms that asked for upgrades")
	// OFF BY DEFAULT, and that is the design rather than caution. A hub that
	// holds a database is a hub whose restart is no longer free, which is the
	// one property this whole split exists to buy.
	c.Flags().BoolVar(&asRoom, "room", false,
		"also run agents on this machine, as a room attached to this hub")
	c.Flags().StringVar(&roomName, "room-name", "",
		"what to call this machine's own room, with --room (default: this machine's name)")
	c.Flags().StringVar(&roomDB, "room-db", "", "its database, with --room")
	c.Flags().StringVar(&roomAgent, "room-agent", "127.0.0.1:7802",
		"where its agents report, with --room")
	c.AddCommand(hubRoomsCmd())
	return c
}

// greet is the first thing anybody sees, and it is the whole setup experience.
//
// WRITTEN AS THE NEXT THING TO DO, not as a status dump. Somebody running this
// for the first time has one question, "now what", and the answer is one line
// they can type.
//
// IT NO LONGER PRINTS A JOIN STRING, and that is the visible half of the hub
// naming its rooms. There is nobody to mint one for until a room has been added
// and given a name, and a string that would enrol anything under any name is
// exactly what went away. An empty hub says what it is and how to give it a
// room, which is a board that explains itself rather than one that quietly
// became something else.
func greet(store *hubstore.Store, side *hubSide, boardAddr, id string) {
	fmt.Println()
	fmt.Println("  the board is at   http://localhost" + portOf(boardAddr))
	fmt.Println("  rooms dial in on  " + side.says)
	fmt.Println("  board build       " + id)
	fmt.Println()

	rooms, err := store.Rooms()
	if err != nil {
		log.Printf("[hub] could not read the room list: %v", err)
		return
	}
	if len(rooms) == 0 {
		fmt.Println("  No rooms yet, and a hub with no room has nothing to show. Give it one:")
		fmt.Println()
		fmt.Println("      atrium2 hub room add <name>")
		fmt.Println()
		fmt.Println("  That prints a join string for that name and nothing else. Paste it into")
		fmt.Println("  `atrium2 join` on the machine your agents are on.")
		fmt.Println()
		return
	}
	fmt.Printf("  %d room(s) on this hub:\n", len(rooms))
	for _, r := range rooms {
		fmt.Println("      " + r.Name + "   " + sinceHeard(r))
	}
	fmt.Println()
	fmt.Println("  `atrium2 hub room ls` says more. `atrium2 hub room add <name>` adds one.")
	fmt.Println()
}

// sinceHeard is a room's line on that list, in the terms somebody reads it in.
func sinceHeard(r hubstore.Room) string {
	switch {
	case r.Marked():
		return "marked for deletion"
	case r.LastSeen == nil:
		// The state the durable list exists for. A room that has never
		// connected is inventory, not work.
		return "never connected"
	case r.LikelyAttached():
		return "attached"
	default:
		return "last heard from " + r.LastSeen.Local().Format("2006-01-02 15:04")
	}
}

// boardFrom picks the board this hub serves and hashes it.
//
// The hash matters more than it looks: the board compares it against what
// `/v1/health` reports and reloads itself when they differ. The hub answers
// that field on the room's behalf, because the hub is the thing serving the
// files. See `rewriteHealth` in internal/link.
func boardFrom(dir string) (fs.FS, string, error) {
	if strings.TrimSpace(dir) == "" {
		return api.EmbeddedBoard(), api.BuildID, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", err
	}
	if _, err := os.Stat(filepath.Join(abs, "index.html")); err != nil {
		return nil, "", fmt.Errorf("%s does not look like a board: no index.html", abs)
	}
	fsys := os.DirFS(abs)
	log.Printf("[hub] serving the board from %s", abs)
	return fsys, api.BoardID(fsys), nil
}

// hubDir is where a hub keeps its certificates.
//
// ITS OWN DIRECTORY, not the one `atrium` uses, so running both on one machine
// cannot have either overwrite the other's state. That is the same rule the
// address file follows and for the same reason.
func hubDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "atrium2", "hub")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".atrium2", "hub")
}

func roomDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "atrium2", "room")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".atrium2", "room")
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// advertised turns a bind address into one a room can dial.
//
// `:7801` binds everything and dials nothing, so a join string carrying it
// would be useless on the machine that pasted it. Loopback is the answer that
// is right for tonight's case, two accounts on one box, and the flag is there
// for when it is not.
func advertised(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return ":" + port
}
