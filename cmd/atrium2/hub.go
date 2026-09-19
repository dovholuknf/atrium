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
		advertise string
		service   string
		files     string
		open      bool

		// Reaching the board from off this machine. Empty binds loopback only,
		// which is the default and the safe one. "zrok" also serves the same
		// board over a zrok share. See hubshare.go.
		boardTransport string
		boardShareMode string

		// Binaries to offer rooms, one per platform. Empty means this hub can
		// only offer what it is running, which is no use to a room on another
		// kind of machine. See `builds.go`.
		buildDir string
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
			// THE BOARD STAYS ON LOOPBACK, checked before anything else opens, so
			// a wide --addr is refused rather than half-started. A wide room link
			// is the operator's choice and is not touched here.
			board, err := loopbackBoard(board)
			if err != nil {
				return err
			}
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
			// A hub from before this build may hold a row for its own machine as
			// a room. That feature is gone, so the row is defunct: drop it.
			if n, err := store.DropDefunctLocalRooms(); err != nil {
				return fmt.Errorf("tidying old rooms: %w", err)
			} else if n > 0 {
				log.Printf("[hub] dropped %d room(s) from when the hub could be its own room", n)
			}

			// SPENDING A SECRET IS WHAT SAYS WHICH ROOM THIS IS, and the store
			// is the only thing that can answer it. Handed to the transport
			// rather than reached for, so `internal/link` never learns the hub
			// has a database.
			side, err := openHub(transport, keys, port, advertise, service,
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
			// Every room proves who it is with a certificate this hub signed.
			h.Authenticated = side.auth
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
			// THE CONTROL MCP SERVER, mounted at /_hub/mcp so sessions open a
			// connection instead of each spawning an atrium-control child. It
			// reaches this hub's own board over loopback derived from `board`.
			proxy.SetControl(board)

			ctx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			go func() {
				if err := h.Serve(ctx, ln); err != nil && ctx.Err() == nil {
					log.Printf("[hub] the room listener stopped: %v", err)
				}
			}()

			// SNAPSHOTS OF ITS OWN STORE, because a halt on a corrupt database
			// is only tolerable if there is something to go back to. Without
			// this, "it halts" means "it is gone".
			//
			// Tiered rather than counted: everything from the last hour, one an
			// hour for a day, one a day for a week, one a week for a month. The
			// two questions people ask are "put it back to twenty minutes ago"
			// and "what did this look like last week", and a flat list of the
			// most recent fifty answers only the first.
			go store.BackUp(ctx, backupsIn(keys.Dir, db))

			// HANDING FREED PAGES BACK TO DISK, a bounded batch at a time while
			// the hub stays live. The room cache is rewritten wholesale on every
			// announce, so its file grows and never shrinks on its own. A no-op
			// on an older store not in incremental mode. See internal/hubstore.
			go store.VacuumLoop(ctx)

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

			// REACHING THE BOARD FROM ELSEWHERE, when asked. Loopback above
			// stays exactly as it was: this adds a second listener on a zrok
			// share and serves the SAME handler on it, so nothing is proxied
			// and the local board is untouched. The share is released when the
			// daemon stops.
			if bt := strings.TrimSpace(boardTransport); bt != "" && bt != "none" {
				if bt != "zrok" {
					return fmt.Errorf("no board transport called %q. one of: zrok", bt)
				}
				// NON-FATAL, ON PURPOSE. The board share is additive: the local
				// board on loopback is the hub's real job and must come up even
				// when the overlay API is slow or down. A share creation that
				// times out at zrok logs the reason and the hub serves loopback
				// anyway, rather than a transient outage taking the board with
				// it.
				bs, shareLn, err := openBoardShare(boardShareMode)
				if err != nil {
					log.Printf("[hub] could not put the board on a zrok share, "+
						"serving loopback only: %v", err)
				} else {
					defer bs.release()
					if bs.Mode == "public" {
						log.Printf("[hub] the board is on a PUBLIC zrok share with no login " +
							"in front of it. whoever opens the link can read every command " +
							"and answer permission requests")
					}
					log.Printf("[hub] serving the board on a %s zrok share: %s", bs.Mode, bs.Address)
					shareSrv := &http.Server{Handler: proxy, ReadHeaderTimeout: 10 * time.Second}
					go func() {
						<-ctx.Done()
						_ = shareSrv.Close()
					}()
					go func() {
						if err := shareSrv.Serve(shareLn); err != nil && ctx.Err() == nil {
							log.Printf("[hub] the board's zrok share stopped: %v", err)
						}
					}()
				}
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
	c.Flags().StringVar(&board, "addr", ":7800", "where the browser reaches the board (loopback only, no login)")
	c.Flags().StringVar(&port, "link", ":7801", "where rooms dial in (may bind wide, e.g. 0.0.0.0:7801)")
	c.Flags().StringVar(&advertise, "link-advertise", "",
		"the host:port a room dials this hub at, minted into join strings (required when --link binds wide)")
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
	c.Flags().StringVar(&boardTransport, "board-transport", "",
		"also serve the board off this machine over an overlay: none (default) or zrok")
	c.Flags().StringVar(&boardShareMode, "board-share", "private",
		"with --board-transport zrok: private (needs zrok on the other end) or public (a URL, no login)")
	c.AddCommand(hubRoomsCmd(), hubBackupsCmd(), hubRestoreCmd())
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

// advertiseFor decides what a join string carries for the room link.
//
// A wide bind is not dialable, so a token minted from it would carry an address
// no room can reach. The rules:
//
//   - An explicit --link-advertise always wins, whatever the bind is.
//   - A loopback bind (127.0.0.1, ::1) keeps 127.0.0.1, which is the default and
//     the two-accounts-on-one-box case.
//   - An explicit wide bind (0.0.0.0, ::) with no advertise is REFUSED rather
//     than coerced to loopback. Coercing it mints a loopback token that looks
//     right and fails for every remote room, which is the silent failure this
//     exists to stop. The operator hands the reachable address on
//     --link-advertise.
//   - An empty host (":PORT") keeps today's loopback default: it is the shape
//     the default flag ships with and the historical two-accounts case relies
//     on it.
func advertiseFor(bind, override string) (string, error) {
	if o := strings.TrimSpace(override); o != "" {
		return o, nil
	}
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return "", fmt.Errorf("--link %q is not a host:port: %w", bind, err)
	}
	if host == "0.0.0.0" || host == "::" {
		return "", fmt.Errorf(
			"--link is bound wide to %s but --link-advertise is not set. rooms cannot "+
				"dial %s, so the join string would be useless. pass --link-advertise "+
				"<host:port> with the address a room reaches this hub at", bind, host)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}

// loopbackBoard checks the board's bind address and pins it to loopback.
//
// THE BOARD HAS NO LOGIN. That is the invariant in the root CLAUDE.md and in
// docs/overlays.md: loopback and no login stays true, and reaching the board
// from elsewhere is an overlay's job, not a wide bind on a port with no auth.
// This makes the invariant enforced rather than assumed.
//
// An empty host (`:7800`) binds every interface, so it is coerced to 127.0.0.1
// rather than refused: the default must stay usable and must also stay safe. An
// explicit non-loopback host is the operator asking for the one thing this
// refuses, so it is refused with the reason and the alternative.
func loopbackBoard(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("--addr %q is not a host:port for the board: %w", addr, err)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if !isLoopbackHost(host) {
		return "", fmt.Errorf(
			"--addr %q would put the board on a non-loopback address, and the board "+
				"has no login, so anything that can reach it can read every command and "+
				"answer permission requests. keep --addr on loopback (127.0.0.1). to reach "+
				"the board from elsewhere use an overlay (--board-transport zrok); to let a "+
				"room dial in from elsewhere bind --link wide instead", addr)
	}
	return net.JoinHostPort(host, port), nil
}

// isLoopbackHost is true for an address that names only this machine.
//
// A loopback IP or the name localhost. A different hostname is refused rather
// than resolved: a name that happens to point at loopback today can point
// elsewhere tomorrow, and the board's safety must not depend on what a resolver
// says at start.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return ":" + port
}
