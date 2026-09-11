package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/spf13/cobra"
)

// A board you can look at without touching the one you work in.
//
// The problem this solves is not tenancy and not scale. It is that changing
// the board means changing files under `internal/api/web/`, and the only way to
// SEE the change was to install the binary and restart the daemon that every
// live session on the machine is attached to. So a session working on the UI
// had to interrupt everybody else to show its work, and the operator had to
// take the interruption before knowing whether the work was any good.
//
// A preview is a whole daemon: its own database, its own ports, and its own
// address file. What makes it safe is the third one.
//
// ADDRESS FILE FIRST, because it is the thing that goes wrong. Every daemon
// records where it is listening, and every hook in every claude session on the
// machine reads that file to find one. A second daemon that writes the same
// file silently steals every hook on the box, and the symptom is not an error:
// it is activity, permissions and session lifecycle arriving at a daemon
// nobody is looking at. `--location-file` already existed for exactly this and
// this subcommand is what makes remembering it unnecessary.
//
// ITS OWN DATABASE, and `--from` COPIES one to start from.
//
// An empty board hides most of what a change to the board did: no columns with
// anything in them, no waiting card to make the alerting visible, no terminal
// to attach to. Judging a layout against that is judging the empty state,
// which is the one state nobody was working on.
//
// A copy rather than the real file, and the difference is not a nicety. Two
// daemons on one sqlite file is two writers, and the preview is the one that
// gets restarted and thrown away. Copying takes the `-wal` and `-shm` with it,
// because sqlite keeps recent writes in the first of those and a database
// copied without it is one that opens and then disagrees with itself.
//
// EPHEMERAL PORTS BY DEFAULT. Twelve worktrees cannot each pick 7778, and
// asking somebody to hand out port numbers is how they collide anyway. The
// operating system knows which ports are free, so it picks, and the address is
// printed because a port nobody can predict is a port somebody has to be told.
//
// WHAT IT IS NOT is a second control plane. Hooks are machine-wide, so a
// session started under a preview still files its activity to the real daemon.
// That is a limitation and not a bug: a preview is for LOOKING at a board.

// previewDir is where a preview keeps its state inside the worktree.
//
// Inside the tree it belongs to, so throwing away the worktree throws away the
// preview with it, and two worktrees cannot share a database by accident.
const previewDir = ".atrium/preview"

func newPreview() *cobra.Command {
	var dir, dbPath, from string
	var httpPort, agentPort int
	var fresh bool

	c := &cobra.Command{
		Use:   "preview",
		Short: "Run a throwaway board for this worktree, without disturbing the real one.",
		Long: "Starts a SECOND atrium: its own database, its own ports, and its own address file, " +
			"so nothing about the daemon you actually use is touched.\n\n" +
			"For looking at a change to the board. Build this worktree, run this, open the " +
			"address it prints, and judge the change by using it rather than by reading a diff.\n\n" +
			"It does NOT take over the hooks. Every claude session on this machine still reports " +
			"to the real daemon, which is what makes running one of these harmless and also what " +
			"stops it being a second place to work.\n\n" +
			"Ports are picked by the operating system unless you name them, so several of these " +
			"can run at once.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPreview(cmd.Context(), previewOpts{
				Dir: dir, DBPath: dbPath, From: from,
				HTTPPort: httpPort, AgentPort: agentPort,
				Fresh: fresh,
			})
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "the worktree this previews (default: the current directory)")
	c.Flags().StringVar(&dbPath, "db", "",
		"where to keep its cards (default: .atrium/preview inside that directory)")
	c.Flags().StringVar(&from, "from", "",
		"copy this database to start from, so the board has real cards on it. "+
			"`live` means the one the running daemon opened. never opened in place")
	c.Flags().IntVar(&httpPort, "http", 0, "board port (default: whatever is free)")
	c.Flags().IntVar(&agentPort, "agent", 0, "agent listener port (default: whatever is free)")
	c.Flags().BoolVar(&fresh, "fresh", false,
		"throw away this preview's previous cards before starting")
	return c
}

type previewOpts struct {
	Dir, DBPath, From   string
	HTTPPort, AgentPort int
	Fresh               bool
}

func runPreview(ctx context.Context, o previewOpts) error {
	dir := strings.TrimSpace(o.Dir)
	if dir == "" {
		got, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = got
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	state := filepath.Join(dir, filepath.FromSlash(previewDir))
	if o.Fresh {
		// The whole state directory, not just the database. A sqlite file
		// leaves a `-wal` and a `-shm` beside it, and removing one of three
		// produces a database that opens and disagrees with itself.
		if err := os.RemoveAll(state); err != nil {
			return fmt.Errorf("could not clear the previous preview: %w", err)
		}
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}

	db := strings.TrimSpace(o.DBPath)
	if db == "" {
		db = filepath.Join(state, "preview.db")
	}

	if from := strings.TrimSpace(o.From); from != "" {
		copied, err := copyForPreview(from, db)
		if err != nil {
			return err
		}
		if copied {
			fmt.Printf("copied %s\n", from)
		}
	}

	// Asked for now rather than left to the listener, because the address has
	// to be PRINTED and a port the operating system picks is not knowable
	// until something has bound it. Held open until the daemon binds, which is
	// the ordinary race here and is why the two ports are taken together.
	human, closeHuman, err := freePort(o.HTTPPort)
	if err != nil {
		return err
	}
	agent, closeAgent, err := freePort(o.AgentPort)
	if err != nil {
		closeHuman()
		return err
	}
	closeHuman()
	closeAgent()

	opts := daemon.Options{
		AgentAddr: fmt.Sprintf("127.0.0.1:%d", agent),
		HumanAddr: fmt.Sprintf("127.0.0.1:%d", human),
		DBPath:    filepath.ToSlash(db),
		LongPoll:  60 * time.Second,
		// THE LINE THAT MAKES THIS SAFE. Without it this daemon records itself
		// as the machine's atrium and every hook in every live session starts
		// arriving here instead of at the one you are working in.
		LocationFile: filepath.Join(state, "daemon.json"),
		// A WINDOW, NOT A SECOND OPERATOR. Everything in a copied database is
		// real, and an ordinary start acts on it: fixtures spawn, lent shares
		// are re-bound, dead ones are swept off the account. The first version
		// of this did exactly that and went after the operator's zrok name
		// from a process meant to be a preview.
		Passive: true,
	}

	fmt.Printf("preview of %s\n", filepath.ToSlash(dir))
	fmt.Printf("  board   http://127.0.0.1:%d\n", human)
	fmt.Printf("  cards   %s\n", filepath.ToSlash(db))
	fmt.Printf("  hooks   untouched. this is not the machine's atrium.\n")
	fmt.Printf("  stop    ctrl-c\n\n")

	return runDaemon(ctx, opts, false)
}

// copyForPreview duplicates a database so the preview has real cards on it.
//
// NEVER OPENS THE SOURCE. Two daemons on one sqlite file is two writers, and
// the preview is the one that gets restarted and thrown away, so it works on a
// copy and the original is only ever read.
//
// THE SIDECARS COME TOO. sqlite in WAL mode keeps recent writes in `-wal` and
// its index in `-shm`, so a database copied on its own is one that opens fine
// and is missing whatever happened most recently, which reads as a board that
// is mysteriously out of date rather than as a bad copy.
//
// Refuses rather than overwrites. A preview that has been used has cards
// somebody may be looking at, and `--fresh` is how you say to throw them away.
func copyForPreview(from, to string) (bool, error) {
	if from == "live" {
		loc, ok := daemon.ReadLocation()
		if !ok || strings.TrimSpace(loc.DB) == "" {
			return false, fmt.Errorf("no running daemon said which database it opened, " +
				"so `--from live` has nothing to copy. name the file instead")
		}
		from = loc.DB
	}
	from = filepath.FromSlash(from)
	if _, err := os.Stat(from); err != nil {
		return false, fmt.Errorf("nothing to copy at %s: %w", from, err)
	}
	if _, err := os.Stat(to); err == nil {
		// Already has one. Said out loud rather than silently kept, because
		// "why are these cards stale" is the question this would otherwise
		// produce an hour later.
		fmt.Printf("  (keeping the cards this preview already had. --fresh to start over)\n")
		return false, nil
	}

	for _, ext := range []string{"", "-wal", "-shm"} {
		src, err := os.ReadFile(from + ext)
		if err != nil {
			// Only the database itself has to be there. A cleanly closed
			// sqlite has no sidecars at all.
			if ext == "" {
				return false, err
			}
			continue
		}
		if err := os.WriteFile(to+ext, src, 0o600); err != nil {
			return false, err
		}
	}
	return true, nil
}

// freePort answers a port that is free right now, or checks the one asked for.
//
// The listener is handed BACK to the caller to close rather than closed here,
// so that two ports can be reserved at once. Taking one and releasing it
// before taking the second is how both end up being the same number.
func freePort(want int) (int, func(), error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", want))
	if err != nil {
		if want != 0 {
			return 0, nil, fmt.Errorf("port %d is not free: %w", want, err)
		}
		return 0, nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	return port, func() { _ = ln.Close() }, nil
}
