package daemon

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An empty board looks the same whether your state is gone or whether you are
// pointed at the wrong file. Only one of those is a problem, and the daemon is
// the only thing that knows which.
//
// This is not hypothetical: WORKTREE_ROOT unset once sent the path to the home
// directory, and a hundred and twenty five rules appeared to have vanished.
func TestNewDatabaseIsAnnouncedLoudly(t *testing.T) {
	_, logs, cancel, _ := startDaemon(t)
	defer cancel()

	out := logs.String()
	if !strings.Contains(out, "THIS IS A NEW DATABASE") {
		t.Fatalf("creating a database said nothing about it:\n%s", out)
	}
	if !strings.Contains(out, "WORKTREE_ROOT") {
		t.Errorf("the warning does not name the usual cause:\n%s", out)
	}
}

// The warning has to be rare enough to mean something. A daemon reopening the
// database it made last time must be silent about it, or the notice becomes
// noise and stops being read.
func TestReopeningADatabaseIsQuiet(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "atrium.db"))
	opts := Options{
		AgentAddr: freePort(t),
		HumanAddr: freePort(t),
		DBPath:    path,
		LongPoll:  2 * time.Second,
	}

	first, err := New(opts)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	first.Close()

	logs := &safeBuf{}
	old := log.Writer()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(old) })

	opts.AgentAddr, opts.HumanAddr = freePort(t), freePort(t)
	second, err := New(opts)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { second.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go second.Run(ctx)
	logs.waitFor(t, "ready. ctrl-c to stop")

	if strings.Contains(logs.String(), "THIS IS A NEW DATABASE") {
		t.Fatalf("reopening an existing database warned anyway:\n%s", logs.String())
	}
}
