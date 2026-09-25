package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/spf13/cobra"
)

// `atrium run`: the atrium in the foreground, and a room for this machine.
//
// TWO PROCESSES, ON PURPOSE. The atrium restarts freely and the room does not,
// and that split is the whole reason there are two. One process holding both
// would make every board fix a restart of every agent. So this serves the
// atrium here and, when this machine has no room answering, starts `atrium
// room` detached. Stopping or restarting `atrium run` never touches the room.
//
// The first run on a fresh machine also makes the room: it names it after the
// machine, mints it a join string against this atrium, and enrols it over the
// link, the way `atrium rooms add` and `atrium room join` do by hand. So one
// machine needs no join string at all.

// roomLogName is where a room `atrium run` started writes its output, beside
// its keys, because nobody is reading a detached process's console.
const roomLogName = "room.log"

func newRun() *cobra.Command {
	var (
		f        atriumFlags
		noRoom   bool
		isolated bool
		l        roomLaunch
	)
	c := &cobra.Command{
		Use:   "run",
		Short: "Run the atrium, and start this machine's room when none is running",
		Long: hubLong + "\n\n" +
			"THE ROOM IS A SEPARATE PROCESS. When this machine has no room answering,\n" +
			"`atrium run` starts `atrium room` detached, and makes the room first if this\n" +
			"machine never had one. A room already running is left alone, and stopping\n" +
			"`atrium run` never stops the room. --no-room serves the atrium alone.\n\n" +
			"The atrium's own directory, store and ziti identity are --atrium-dir,\n" +
			"--atrium-db, --atrium-identity and --atrium-service. --dir, --db, --http and\n" +
			"--agent are the room's, exactly as `atrium room` takes them.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if noRoom {
				return serveAtrium(f, nil)
			}
			l.dir = orDefault(l.dir, roomDir())
			l.db = orDefault(l.db, defaultRoomDB())
			l.isolated = isolated
			l.upgrades = acceptUpgrades
			return serveAtrium(f, func(store *hubstore.Store, _ link.Keys, side *hubSide) error {
				return ensureRoom(store, side, f.transport, l)
			})
		},
	}
	f.bind(c, "atrium-")
	c.Flags().BoolVar(&noRoom, "no-room", false,
		"serve the atrium alone, and never look for or start a room")
	c.Flags().StringVar(&l.dir, "dir", "", "where this machine's room keeps its certificate")
	c.Flags().StringVar(&l.db, "db", "", "the room's database")
	c.Flags().StringVar(&l.human, "http", defaultRoomHTTP(), "the room's own board, for when the atrium is down")
	c.Flags().StringVar(&l.agent, "agent", defaultRoomAgent(), "where the room's agents report")
	isolatedFlag(c, &isolated)
	acceptUpgradeFlag(c)
	return c
}

// ensureRoom makes sure this machine has a room running, and changes nothing
// when it already does.
func ensureRoom(store *hubstore.Store, side *hubSide, transport string, l roomLaunch) error {
	loc := roomAddressFile(l)
	if board, ok := roomAnswers(loc); ok {
		log.Printf("[hub] this machine's room answers at %s. left alone", board)
		return nil
	}
	keys := link.Keys{Dir: l.dir}
	if _, err := keys.Joined(); err != nil {
		if err := mintOwnRoom(store, side, transport, keys); err != nil {
			return fmt.Errorf("could not make this machine's room: %w. `atrium rooms add` "+
				"and `atrium room join` do it by hand", err)
		}
	}
	p, logPath, err := startRoom(l)
	if err != nil {
		return fmt.Errorf("could not start `atrium room`: %w", err)
	}
	log.Printf("[hub] started this machine's room, pid %d, logging to %s", p.Pid, logPath)
	return p.Release()
}

// roomAddressFile is where the room this would start writes its address, which
// is the same answer runRoom arrives at: a location already in the environment
// wins, because the room inherits it.
func roomAddressFile(l roomLaunch) string {
	if p := strings.TrimSpace(os.Getenv("ATRIUM_LOCATION")); p != "" {
		return p
	}
	loc, _ := roomLocationEnv(l.isolated, l.dir)
	return loc
}

// roomAnswers reads a room's address file and asks its board whether it is up.
//
// THE BOARD, NOT THE PID. A file left by a room that was killed names a pid
// that may since belong to something else, and a port that answers health is
// the one thing a second room could not also claim.
func roomAnswers(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var loc daemon.Location
	if err := json.Unmarshal(raw, &loc); err != nil || strings.TrimSpace(loc.Board) == "" {
		return "", false
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(strings.TrimRight(loc.Board, "/") + "/v1/health")
	if err != nil {
		return loc.Board, false
	}
	resp.Body.Close()
	return loc.Board, resp.StatusCode == http.StatusOK
}

// mintOwnRoom names a room after this machine, mints it a join string on this
// atrium, and enrols it over the link, saving its keys in keys.Dir.
//
// DIRECT ONLY. Over ziti or zrok the network decides who may connect, and
// this process cannot enrol an identity on a controller it does not run.
func mintOwnRoom(store *hubstore.Store, side *hubSide, transport string, keys link.Keys) error {
	if t := strings.TrimSpace(transport); t != "" && t != hubstore.TransportDirect {
		return fmt.Errorf("the room link is over %s, and only a direct link can make a room "+
			"on its own", t)
	}
	name := defaultRoomName()
	r, err := store.ByName(name)
	if err != nil {
		if r, err = store.Add(name, hubstore.TransportDirect); err != nil {
			return err
		}
	}
	secret, err := store.Mint(r.ID)
	if err != nil {
		return err
	}
	line, err := side.joinString(r.Name, secret)
	if err != nil {
		return err
	}
	j, err := link.ParseToken(line)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d := link.Direct{Addr: j.Addr, Keys: keys, Pin: j.Pin}
	got, err := d.Enrol(ctx, defaultRoomName(), j.Secret)
	if err != nil {
		return err
	}
	log.Printf("[hub] made this machine's room %q, keys in %s", got, keys.Dir)
	return nil
}

// startRoom runs `atrium room` detached with the whole launch, the same line
// a hub-triggered restart would run, and its output going to room.log.
func startRoom(l roomLaunch) (*os.Process, string, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return nil, "", err
	}
	logPath := filepath.Join(l.dir, roomLogName)
	out, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, "", err
	}
	defer out.Close()
	p, err := startDetached(self, l.startArgs(), out)
	return p, logPath, err
}
