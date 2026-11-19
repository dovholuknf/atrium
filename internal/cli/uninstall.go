package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/addrfile"
	"github.com/dovholuknf/atrium/internal/base/claudeconf"
	"github.com/dovholuknf/atrium/internal/base/nowindow"
	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/ptyhost"
	"github.com/spf13/cobra"
)

type uninstallOpts struct {
	purge, dryRun, force bool
	services             []string
	stateDir, exe        string
	outside              []string
}

// uninstallDeps are the effects an uninstall has, so a test can plan against a temp state dir and touch nothing live.
type uninstallDeps struct {
	heldTerminals func() int
	stop          func(runners bool) error
	hasHooks      func() bool
	removeHooks   func() error
	removeService func(name string) error
}

func newUninstall() *cobra.Command {
	var o uninstallOpts
	c := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop atrium, take it out of claude's settings, and say what is left behind.",
		Long: "Stops the hub and room this user started, removes atrium's hooks from claude's settings (as " +
			"`atrium hook uninstall` does), removes the services named by --service, then prints the state folder " +
			"and the binary it left behind.\n\n" +
			"Refused while the pty host holds live terminals, unless --force, which ends them.\n\n" +
			"--purge also deletes the state folder, never the binary running this and never a git worktree in it " +
			"with uncommitted work, which are listed and kept. --dry-run prints the plan and changes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.stateDir = daemon.StateDir()
			o.exe, _ = os.Executable()
			if p, err := addrfile.DefaultLocationPath(); err == nil {
				o.outside = []string{filepath.Join(filepath.Dir(p), "lastdb.json"), p}
			}
			return runUninstall(cmd.OutOrStdout(), o, liveUninstallDeps(o.stateDir))
		},
	}
	c.Flags().BoolVar(&o.purge, "purge", false, "also delete atrium's state folder, keeping any worktree with uncommitted work")
	c.Flags().BoolVar(&o.dryRun, "dry-run", false, "print the plan and change nothing")
	c.Flags().BoolVar(&o.force, "force", false, "go on although the pty host holds live terminals, ending them")
	c.Flags().StringArrayVar(&o.services, "service", nil, "a Windows service `name` registered with `atrium service install`, repeatable")
	return c
}

func liveUninstallDeps(stateDir string) uninstallDeps {
	return uninstallDeps{
		heldTerminals: func() int {
			cl, err := ptyhost.DialTimeout(ptyhost.Address(stateDir), ptyhost.DefaultDialTimeout)
			if err != nil {
				return 0
			}
			defer cl.Close()
			p, err := cl.Probe()
			if err != nil {
				return 0
			}
			return p.Ptys
		},
		stop: func(runners bool) error {
			loc, ok := daemon.ReadLocation()
			hub := hubBoardAddress("")
			hubUp := boardListening(hub) && (!ok || !sameBoard(hub, loc.Board))
			if !ok && !hubUp {
				return errors.New("no running atrium is recorded on this machine")
			}
			var errs []error
			if ok {
				errs = append(errs, stopDaemon(loc.Board, "", false, runners))
			}
			if hubUp {
				errs = append(errs, stopDaemon(hub, "", false, false))
			}
			waitStopped(loc.PID, hub, 60*time.Second)
			return errors.Join(errs...)
		},
		hasHooks:      func() bool { return claudeconf.HasAtriumHooks(claudeconf.Claude) },
		removeHooks:   func() error { _, err := claudeconf.UninstallTarget(claudeconf.Claude); return err },
		removeService: uninstallService,
	}
}

func runUninstall(out io.Writer, o uninstallOpts, d uninstallDeps) error {
	held := d.heldTerminals()
	if held > 0 && !o.force {
		fmt.Fprintf(out, "the pty host holds %d live terminal(s). stopping would leave them running without atrium, "+
			"and --force ends them. nothing was changed.\n", held)
		return errAlreadySaid
	}
	say := func(plan, done string) string {
		if o.dryRun {
			return "would " + plan
		}
		return done
	}
	step := func(plan, done string, do func() error) {
		fmt.Fprintln(out, say(plan, done))
		if o.dryRun {
			return
		}
		if err := do(); err != nil {
			fmt.Fprintf(out, "  %v\n", err)
		}
	}

	step("stop the hub and room", "stopping the hub and room", func() error { return d.stop(o.force && held > 0) })
	if d.hasHooks() {
		step("remove atrium's hooks from claude's settings", "removing atrium's hooks from claude's settings", d.removeHooks)
	}
	for _, name := range o.services {
		step("remove the service "+name, "removing the service "+name, func() error { return d.removeService(name) })
	}

	kept, err := dirtyWorktrees(o.stateDir)
	if err != nil {
		return err
	}
	if o.purge {
		if o.exe != "" {
			kept = append(kept, o.exe)
		}
		fmt.Fprintln(out, say("delete "+o.stateDir, "deleting "+o.stateDir))
		if !o.dryRun {
			if err := purgeRetrying(o.stateDir, kept, 30*time.Second); err != nil {
				return err
			}
			for _, f := range o.outside {
				_ = os.Remove(f)
			}
			if len(o.outside) > 0 {
				_ = os.Remove(filepath.Dir(o.outside[0]))
			}
		}
		for _, k := range kept {
			fmt.Fprintf(out, "  keeping %s\n", filepath.ToSlash(k))
		}
		fmt.Fprintf(out, "left behind: the binary %s\n", filepath.ToSlash(o.exe))
		return nil
	}
	fmt.Fprintf(out, "left behind: the state folder %s (--purge deletes it) and the binary %s\n",
		filepath.ToSlash(o.stateDir), filepath.ToSlash(o.exe))
	return nil
}

func boardListening(board string) bool {
	u, err := url.Parse(board)
	if err != nil || u.Host == "" {
		return false
	}
	c, err := net.DialTimeout("tcp", u.Host, time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// waitStopped waits, up to limit, for the room's process to exit and the hub's board to stop accepting, so a purge
// does not meet their open stores.
func waitStopped(roomPID int, hub string, limit time.Duration) {
	for end := time.Now().Add(limit); time.Now().Before(end); time.Sleep(250 * time.Millisecond) {
		if !pidAlive(roomPID) && !boardListening(hub) {
			return
		}
	}
}

// purgeRetrying is purgeExcept tried again for up to limit, because Windows refuses to delete a file a process that
// is still exiting holds open.
func purgeRetrying(dir string, keep []string, limit time.Duration) error {
	end := time.Now().Add(limit)
	for {
		err := purgeExcept(dir, keep)
		if err == nil || time.Now().After(end) {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// dirtyWorktrees lists git worktrees under dir whose status is not clean, or cannot be read. Neither is ever deleted.
func dirtyWorktrees(dir string) ([]string, error) {
	var dirty []string
	err := filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		if !e.IsDir() {
			return nil
		}
		if _, err := os.Stat(filepath.Join(p, ".git")); err != nil {
			return nil
		}
		cmd := exec.Command("git", "-C", p, "status", "--porcelain")
		nowindow.Hide(cmd)
		o, err := cmd.Output()
		if err != nil || strings.TrimSpace(string(o)) != "" {
			dirty = append(dirty, p)
		}
		return filepath.SkipDir
	})
	return dirty, err
}

func isKept(p string, keep []string) bool {
	for _, k := range keep {
		if k == p {
			return true
		}
	}
	return false
}

// purgeExcept deletes everything under dir except each path in keep and the folders that hold one.
func purgeExcept(dir string, keep []string) error {
	holds := func(p string) bool {
		for _, k := range keep {
			if k == p || strings.HasPrefix(k, p+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	if !holds(dir) {
		return os.RemoveAll(dir)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		p := filepath.Join(dir, e.Name())
		switch {
		case !holds(p):
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		case e.IsDir() && !isKept(p, keep):
			if err := purgeExcept(p, keep); err != nil {
				return err
			}
		}
	}
	return nil
}
