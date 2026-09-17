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
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/spf13/cobra"
)

// The hub: serve the board, hold nothing, hand everything else to a room.

func hubCmd() *cobra.Command {
	var (
		board     string
		port      string
		dir       string
		transport string
		service   string
		files     string
		open      bool

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
			"THE HUB HOLDS NOTHING. No database, no cards, no scrollback. Everything the\n" +
			"board shows comes from a room, live. That is what makes it safe to restart\n" +
			"while somebody's agent is mid-sentence.\n\n" +
			"On first run it makes itself a certificate authority and prints a join string.\n" +
			"Paste that into `atrium2 join` on the machine your agents are on.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys := link.Keys{Dir: orDefault(dir, hubDir())}
			side, err := openHub(transport, keys, port, service)
			if err != nil {
				return err
			}
			defer side.release()

			ln, err := side.listen()
			if err != nil {
				return err
			}
			defer ln.Close()

			h := link.NewHub(link.Timings{})
			// WHAT THIS HUB IS RUNNING, so a room that asked to be told can be
			// told. Hashed once here rather than per attach: it is fifty
			// megabytes and it cannot change while this process is running.
			//
			// A failure is logged and ignored. Not being able to describe your
			// own binary is not a reason to refuse to serve a board.
			if o, err := link.Offered(version); err != nil {
				log.Printf("[hub] cannot describe this binary, so rooms will not be offered it: %v", err)
			} else {
				h.Offers(o)
			}
			h.Enrol = side.enrol
			// A ROOM IN THIS PROCESS IS NOT ASKED FOR PAPERS. Every other room
			// proves who it is with a certificate this hub signed, because its
			// connection crossed a network. That one did not leave the
			// process. See `link.IsInProc`.
			h.Authenticated = func(c net.Conn) bool {
				return link.IsInProc(c) || side.auth(c)
			}

			// The board this hub serves. From disk when told to, so the loop is
			// edit, save, reload, with no rebuild at all.
			assets, id, err := boardFrom(files)
			if err != nil {
				return err
			}
			proxy := link.NewProxy(h, assets, id, nil)

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
				parent: ctx, hub: h, dir: keys.Dir,
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

			greet(side, board, id)

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
	c.Flags().StringVar(&transport, "transport", "direct",
		"how rooms reach this hub: direct, ziti or zrok")
	c.Flags().StringVar(&service, "service", "atrium-hub", "the ziti service, with --transport ziti")
	c.Flags().StringVar(&zitiIdentity, "identity", "", "the ziti identity file, with --transport ziti")
	c.Flags().StringVar(&files, "board", "",
		"serve the board from this directory instead of the built-in copy")
	c.Flags().BoolVar(&open, "open", false, "print the address and nothing else")
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
	c.AddCommand(tokenCmd())
	return c
}

// tokenCmd mints another join string, for the second room or a lost one.
func tokenCmd() *cobra.Command {
	var dir, port string
	c := &cobra.Command{
		Use:   "token",
		Short: "Print a fresh join string",
		Long: "Join strings are good once and for an hour. This prints another without\n" +
			"disturbing a running hub, which is what you want for a second room or\n" +
			"for one you pasted into the wrong window.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys := link.Keys{Dir: orDefault(dir, hubDir())}
			if _, err := keys.Fingerprint(); err != nil {
				return fmt.Errorf("this machine is not a hub yet. run `atrium2 hub` first")
			}
			tok, err := keys.MintToken(advertised(port))
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), tok)
			return nil
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "where this hub keeps its certificates")
	c.Flags().StringVar(&port, "link", ":7801", "where rooms dial in")
	return c
}

// greet is the first thing anybody sees, and it is the whole setup experience.
//
// WRITTEN AS THE NEXT THING TO DO, not as a status dump. Somebody running this
// for the first time has one question, "now what", and the answer is one line
// they can select with a double click.
func greet(side *hubSide, boardAddr, id string) {
	tok, err := side.token()
	if err != nil {
		log.Printf("[hub] could not mint a join string: %v", err)
		return
	}
	fmt.Println()
	fmt.Println("  the board is at   http://localhost" + portOf(boardAddr))
	fmt.Println("  rooms dial in on  " + side.says)
	fmt.Println("  board build       " + id)
	fmt.Println()
	fmt.Println("  Nothing is on it yet, because a hub holds nothing. On the machine your")
	fmt.Println("  agents run on, paste this:")
	fmt.Println()
	fmt.Println("      atrium2 join " + tok)
	fmt.Println()
	fmt.Println("  It is good once and for an hour. `atrium2 hub token` prints another.")
	fmt.Println()
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
