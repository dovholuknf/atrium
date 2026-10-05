package cli

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/dovholuknf/atrium/internal/roomstats"
	"github.com/dovholuknf/atrium/internal/store"
	"github.com/spf13/cobra"
)

// The room: everything that owns something.
//
// A room is an ordinary atrium daemon. It has the store, the pseudo terminals
// and the agents, and it serves its board on loopback exactly as it always did.
// It runs the same binary the hooks run, so the address file it writes names a
// binary that answers `atrium hook`, and the board's "install hooks" writes lines
// that run. See internal/claudeconf/whichexe.go.
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
		noRun    bool
		flags    joinFlags
	)
	c := &cobra.Command{
		Use:   "join <join string>",
		Short: "Run the agents here and attach them to a hub",
		Long: "Takes the line `atrium rooms add` printed, and does everything else.\n\n" +
			"It makes this room a key, gets a certificate from that hub, saves both, and\n" +
			"then runs. The private key never leaves this machine: the hub signs a request\n" +
			"and never sees the key that made it.\n\n" +
			"THE HUB DECIDES WHAT THIS ROOM IS CALLED. The name was chosen when the room\n" +
			"was added there and it travels in the join string, so there is nothing to\n" +
			"pick here. This machine's own name is still reported, and shown beside it.\n\n" +
			"After the first time, `atrium room` runs with what was saved and needs no\n" +
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

			// A PATH TO A .jwt IS READ, so a provisioner can hand the token over
			// as a file rather than as an argument anything on the machine can
			// list. The flag's help always said a path was fine.
			if jwt != "" {
				if raw, err := os.ReadFile(jwt); err == nil {
					jwt = strings.TrimSpace(string(raw))
				}
			}
			if jwt != "" {
				// ENROLLING A JWT IN PLACE, the design's room-link ziti path. The
				// token is turned into an identity `.json` kept on THIS room's own
				// disk, beside its certificate, and that file is what every later
				// dial uses. This is the same shell-out the board-exposure path
				// makes (internal/daemon.EnrollZiti); the identity name is the
				// room's own so a machine with more than one is told them apart.
				// See docs/fabric/ziti-zrok-flow-design.md, "atrium2 join enrolling a JWT
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

			// DIRECT ENROLS OVER TCP, and a new-form ziti or zrok string enrols over
			// its overlay. An old-form overlay string has nothing to enrol: joining
			// is writing down where the hub is and starting.
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
			} else {
				// A NEW-FORM STRING CARRIES A SECRET AND A PIN, and the room
				// spends it OVER the overlay to get the hub's certificate, so
				// every later connection proves its name. An old-form string has
				// neither and does exactly what it always did.
				if j.Proven() {
					raw, err := rawRoomDialer(j, keys, identity)
					if err != nil {
						return err
					}
					ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
					name, err = link.EnrolOver(ctx, raw, keys, j.Pin, self, j.Secret)
					cancel()
					if err != nil {
						return err
					}
				}
				if err := keys.SaveOverlayRoom(j, name, identity); err != nil {
					return err
				}
			}
			fmt.Println()
			fmt.Println("  joined over " + j.Transport + " as \"" + name + "\".")
			if !strings.EqualFold(name, self) {
				fmt.Println("  this machine calls itself \"" + self + "\", which the hub shows beside it.")
			}
			// ENROL AND STOP, for a provisioner that runs the room from a service
			// afterwards rather than from the shell that joined. Without it the
			// join holds the ssh session open for as long as the room lives. See
			// scripts/provision-room.ps1.
			if noRun {
				fmt.Println("  saved. `" + atriumCmd("room") + "` runs it.")
				return nil
			}
			fmt.Println("  starting the room. `" + atriumCmd("room") + "` is all it takes from now on.")
			fmt.Println()
			return runRoom(keys, db, human, agent, 0, isolated, "")
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "where this room keeps its certificate")
	c.Flags().StringVar(&db, "db", "", "the room's database")
	c.Flags().StringVar(&human, "http", defaultRoomHTTP(), "the room's own board, for when the hub is down")
	c.Flags().StringVar(&agent, "agent", defaultRoomAgent(), "where this room's agents report")
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
	c.Flags().BoolVar(&noRun, "no-run", false,
		"join and save, then exit rather than running the room, for a service to run it")
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
		"take a newer atrium from the hub when it has one, verify it, and restart")
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

// startedByFlag registers --started-by on a command that runs a daemon.
func startedByFlag(c *cobra.Command, v *string) {
	c.Flags().StringVar(v, "started-by", "",
		"<kind>:<nonce> a supervisor gives the process it starts. Kept in memory and reported by "+
			"POST /v1/preflight. Never put in the environment or handed to a child")
}

func roomCmd() *cobra.Command {
	var dir, db, human, agent, startedBy string
	var isolated, detach bool
	c := &cobra.Command{
		Use:   "room",
		Short: "Run the agents here, attached to the hub this machine already joined",
		Long: "Runs a full atrium: the database, the terminals and the agents.\n\n" +
			"It also serves its own board on loopback. That is not a leftover, it is the\n" +
			"escape hatch: when the hub is down or you broke it, the room is still a\n" +
			"working atrium at its own address and your agents never noticed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys := link.Keys{Dir: orDefault(dir, roomDir())}
			if detach {
				return detachRoom(roomLaunch{
					dir: keys.Dir, db: db, human: human, agent: agent,
					isolated: isolated, upgrades: acceptUpgrades,
				})
			}
			err := runRoom(keys, db, human, agent, restartAfter, isolated, startedBy)
			// A restarted room that fails to come back has nobody reading its
			// output, so the reason goes in restart.log too. See restartLog.
			if err != nil && restartAfter > 0 {
				restartLog(keys.Dir, "the restarted room failed: %v", err)
			}
			return err
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "where this room keeps its certificate")
	c.Flags().StringVar(&db, "db", "", "the room's database")
	c.Flags().StringVar(&human, "http", defaultRoomHTTP(), "the room's own board, for when the hub is down")
	c.Flags().StringVar(&agent, "agent", defaultRoomAgent(), "where this room's agents report")
	isolatedFlag(c, &isolated)
	startedByFlag(c, &startedBy)
	c.Flags().BoolVar(&detach, "detach", false,
		"start the room in the background, logging to room.log beside --dir, and return once it answers")
	// Hidden: how a hub-triggered restart re-invokes this room detached. It waits
	// for the old process to release its ports, then starts as usual. Running it
	// by hand just adds a pointless pause. See restart.go.
	c.Flags().DurationVar(&restartAfter, "restart-after", 0, "internal: wait this long for the old room to exit first")
	_ = c.Flags().MarkHidden("restart-after")
	acceptUpgradeFlag(c)
	// `atrium room join <string>`, the first time. `atrium join` is the
	// session command the /atrium-join skill runs, so a room's join lives here.
	c.AddCommand(joinCmd())
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
func runRoom(keys link.Keys, db, human, agent string, restartAfter time.Duration, isolated bool, startedBy string) error {
	// A HUB-TRIGGERED RESTART GOT HERE DETACHED, and the old room may still hold
	// the ports. Wait for it to let go before anything tries to bind, or the new
	// room fails to listen and exits, which looks like the restart doing nothing.
	if restartAfter > 0 {
		restartLog(keys.Dir, "restarter up: waiting for the previous room to release %s", human)
		if waitForRoomRestart(restartAfter, human) {
			restartLog(keys.Dir, "%s is free, starting the room", human)
		} else {
			restartLog(keys.Dir, "%s still held after %s, starting anyway", human, roomStopGrace)
		}
	}
	raisePriority()

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
		// In memory only. It is reported by /v1/preflight and by nothing else:
		// not exported, and not carried into a restart or a detached start,
		// because those are children of this process and must not claim to be
		// the supervisor's.
		StartedBy: startedBy,
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
		Version: Version,
		Host:    hostname(),
		// SAYS GIT in the hello, so the hub will ask this room to sync. See internal/gitsync.
		Git: true,
		// IDLE CPU rides on the heartbeat, so the hub can break a tie between rooms with the same session count.
		IdleCPU: roomstats.NewIdleMeter().Idle,
		// WHETHER THIS ROOM WILL TAKE A BUILD ITS HUB IS RUNNING, which is the
		// operator's decision and is off unless they made it. A hub can always
		// SAY what it has; nothing happens here unless this is on. See
		// `internal/link/upgrade.go`.
		Upgrades: &link.Upgrades{
			Accept:  acceptUpgrades,
			Version: Version,
			// WHAT THIS ROOM IS RUNNING, so it cannot be talked into
			// installing itself. See `Upgrades.SHA256`.
			SHA256:  selfSHA(Version),
			Dir:     selfDir(),
			Install: installUpgrade,
		},
		// WHAT THIS ROOM DOES WHEN ITS HUB ASKS IT TO RESTART. Park the other
		// agents, spawn a detached restarter that outlives this process, and wind
		// down. The hub only forwards the ask. See restart.go.
		OnRestart: onHubRestart("http://"+human, roomLaunch{
			dir: keys.Dir, db: db, human: human, agent: agent,
			isolated: isolated, upgrades: acceptUpgrades,
		}, stop),
	}
	// MESSAGES TO OTHER ROOMS go through this link, and what is owed goes the
	// moment it reattaches. See internal/daemon/relay.go.
	// MARKED AS THE LINK, so a terminal attach knows the hub's edge checked it. See internal/edge.
	room.Handler = edge.MarkLink(roomHandler(d, room))
	room.OnAttach = func() {
		d.RelayAttached()
		// PR rows made while the hub could not be reached ask for their claim now. Event driven, no timer.
		d.ReconcilePRClaims()
	}
	d.SetPRClaim(func(ctx context.Context, ask api.PRClaimAsk) (api.PRClaimReply, error) {
		ans, err := room.ClaimPR(ctx, ask)
		if err != nil {
			return api.PRClaimReply{}, err
		}
		return api.PRClaimReply{Owner: ans.Owner, Mine: strings.EqualFold(ans.Owner, room.Name),
			Forwarded: ans.Forwarded, ForwardStatus: ans.ForwardStatus, Forward: ans.Forward}, nil
	})
	d.SetRelay(linkRelay{room: room})
	// THE HUB RUNS THE FORGE. This room asks it about pull requests and issues and never runs gh or bb itself.
	d.SetHubForge(forge.NewRemote(room.Forge))
	// The room's stable hub remote forwards over the same link's git kind.
	d.SetHubGit(func() (http.RoundTripper, error) {
		if !room.HubServesGit() {
			return nil, link.ErrNoGit
		}
		return room.GitTransport(), nil
	})
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

	// Git children hold ref locks, so they are cancelled and waited for as soon as the wind-down
	// starts, inside the daemon's own bound.
	go func() {
		<-ctx.Done()
		log.Printf("[git] stopping git children")
		if !gitsync.Default.Stop(5 * time.Second) {
			log.Printf("[git] a git child did not exit in 5s")
		}
	}()
	return d.Run(ctx)
}

// roomHandler is the board with the room's git surface in front of it: /v1/git/ is sync,
// its status, and upload-pack over the clones. Clones live under the git_root setting,
// which is set on the room and never by the hub, and defaults to ~/git.
//
// IT IS THE LINK'S HANDLER ONLY, set as room.Handler, which serves connections the hub asked
// for. The room's own human listener and a lent session's guest listener use
// d.BoardHandler() directly and have no /v1/git/ at all. The hub's board proxy refuses the
// path as well, so the hub's own sync and collect are the only way in. And only a repository
// the hub has synced since this room started is served (gitsync.Syncer.Served).
func roomHandler(d *daemon.Daemon, room *link.Room) http.Handler {
	sy := &gitsync.Syncer{Root: func() string {
		if v, err := d.Store().Setting(gitsync.SettingGitRoot); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return gitsync.DefaultRoot()
	}, HubRemote: func(canonical string) string { return d.HubRemoteURL(canonical) }}
	gh := &gitsync.RoomHandler{Syncer: sy, Hub: func() (http.RoundTripper, error) {
		if !room.HubServesGit() {
			return nil, link.ErrNoGit
		}
		return room.GitTransport(), nil
	}, Live: func(name string) []string { return liveBranches(d, name) }}
	git, board := gh.Handler(), d.BoardHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/git/") {
			git.ServeHTTP(w, r)
			return
		}
		board.ServeHTTP(w, r)
	})
}

// liveBranches is the branches of this room's live cards in the repository `name` (`github/o/r`), which the
// room's git route serves beside claude/*. It is asked on every fetch, so a card that ends stops being served at
// once.
func liveBranches(d *daemon.Daemon, name string) []string {
	tasks, err := d.Store().List(store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission)
	if err != nil {
		return nil
	}
	return selectLive(name, tasks)
}

// selectLive picks the branches of the live cards in a repository. LIVE IS THE CARD'S STATUS, not whether a process
// is running: a card is live while it is running or waiting on somebody, and a PARKED card (no process, its status
// kept) is still live, so a review it owes keeps being served until it ends. A done, shelved, dead or backlog card
// is not live (a backlog card has no branch of its own yet). A card is matched to its repository by gitsync.RepoMatches.
func selectLive(name string, tasks []*store.Task) []string {
	var out []string
	for _, t := range tasks {
		switch t.Status {
		case store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission:
		default:
			continue
		}
		if t.Branch != "" && gitsync.RepoMatches(name, t.Host, t.Org, t.Repo) {
			out = append(out, t.Branch)
		}
	}
	return out
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
