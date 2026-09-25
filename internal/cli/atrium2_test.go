package cli

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
)

// asShim builds the atrium2 root the way `cmd/atrium2` does, and puts the one
// binary back when the test ends.
func asShim(t *testing.T) *cobra.Command {
	t.Helper()
	legacy = true
	t.Cleanup(func() { legacy = false })
	return newAtrium2Root()
}

// parses finds the command a line names and parses its flags the way cobra
// would, without running it. Returns the command so a test can read values.
func parses(t *testing.T, root *cobra.Command, line string) *cobra.Command {
	t.Helper()
	cmd, rest, err := root.Find(strings.Fields(line))
	if err != nil || cmd == root {
		t.Fatalf("%q names no command: %v", line, err)
	}
	if err := cmd.ParseFlags(rest); err != nil {
		t.Fatalf("%q does not parse: %v", line, err)
	}
	if err := cmd.ValidateArgs(cmd.Flags().Args()); err != nil {
		t.Fatalf("%q has the wrong arguments: %v", line, err)
	}
	return cmd
}

func flag(t *testing.T, cmd *cobra.Command, name string) string {
	t.Helper()
	f := cmd.Flags().Lookup(name)
	if f == nil {
		t.Fatalf("`%s` has no --%s", cmd.CommandPath(), name)
	}
	return f.Value.String()
}

// THE LIVE SCRIPTS' EXACT LINES, as ~/.atrium2/scripts runs them today.
//
// Nothing may break between the merge and the cutover, and these are what
// deploy-batch.ps1, deploy-hub-only.ps1, maintenance-window.ps1,
// start-atrium-hub.ps1 and start-atrium-room.ps1 start. Each must land on the
// command it always did, with every value where it always went.
func TestTheShimAnswersTheLiveScriptsExactLines(t *testing.T) {
	root := asShim(t)

	hub := parses(t, root, `hub --addr 127.0.0.1:7778 --link 0.0.0.0:7779 --link-advertise 192.168.1.68:7779 `+
		`--dir C:\Users\claude\.atrium2\hub`)
	if hub.Name() != "hub" || hub.Parent() != root {
		t.Fatalf("the hub line ran `%s`", hub.CommandPath())
	}
	for name, want := range map[string]string{
		"addr": "127.0.0.1:7778", "link": "0.0.0.0:7779", "link-advertise": "192.168.1.68:7779",
		"dir": `C:\Users\claude\.atrium2\hub`,
	} {
		if got := flag(t, hub, name); got != want {
			t.Errorf("atrium2 hub --%s is %q, want %q", name, got, want)
		}
	}

	room := parses(t, root, `room --dir C:\Users\claude\.atrium2\room --db C:\Users\claude\.atrium\atrium.db `+
		`--http 127.0.0.1:7781 --agent 127.0.0.1:7777`)
	if room.CommandPath() != "atrium2 room" {
		t.Fatalf("the room line ran `%s`", room.CommandPath())
	}
	for name, want := range map[string]string{
		"dir": `C:\Users\claude\.atrium2\room`, "db": `C:\Users\claude\.atrium\atrium.db`,
		"http": "127.0.0.1:7781", "agent": "127.0.0.1:7777",
	} {
		if got := flag(t, room, name); got != want {
			t.Errorf("atrium2 room --%s is %q, want %q", name, got, want)
		}
	}

	// And the rest of what atrium2 answered: what a hub-triggered restart
	// re-runs, sgg's join, the inventory, and the offline tools.
	for line, path := range map[string]string{
		`room --restart-after 4s --dir C:\r --http 127.0.0.1:7781 --agent 127.0.0.1:7777 --db C:\r.db --accept-upgrades`: "atrium2 room",
		`join TOKEN --identity C:\s.json --dir C:\r --db C:\r.db --http 127.0.0.1:7781 --agent 127.0.0.1:7777`:           "atrium2 join",
		`hub room add sgg --transport ziti --service atrium-hub`:                                                         "atrium2 hub room add",
		`hub room ls --dir C:\h`:          "atrium2 hub room ls",
		`hub backups --dir C:\h`:          "atrium2 hub backups",
		`hub restore C:\h\backups\x.db`:   "atrium2 hub restore",
		`db compact --in a.db --out b.db`: "atrium2 db compact",
		`ledger --db C:\r.db --json`:      "atrium2 ledger",
		`version`:                         "atrium2 version",
		`hook --event tool-start`:         "atrium2 hook",
		`room join TOKEN --dir C:\r`:      "atrium2 room join",
	} {
		if got := parses(t, root, line).CommandPath(); got != path {
			t.Errorf("%q ran `%s`, want `%s`", line, got, path)
		}
	}
}

// The one binary's names for the same things, as docs/one-atrium-cutover.md
// has the new scripts run them.
func TestTheOneBinaryAnswersTheNewNames(t *testing.T) {
	root := newRoot()

	run := parses(t, root, `run --no-room --addr 127.0.0.1:7778 --link 0.0.0.0:7779 `+
		`--link-advertise 192.168.1.68:7779 --atrium-dir C:\Users\claude\.atrium2\hub`)
	if got := flag(t, run, "atrium-dir"); got != `C:\Users\claude\.atrium2\hub` {
		t.Errorf("atrium run --atrium-dir is %q", got)
	}
	if got := flag(t, run, "no-room"); got != "true" {
		t.Errorf("atrium run --no-room is %q", got)
	}
	// The room's flags on `run` are the room's, and the atrium's carry the
	// prefix, so one line can name both directories.
	both := parses(t, root, `run --atrium-dir C:\h --atrium-db C:\h\hub.db --atrium-identity C:\z.json `+
		`--atrium-service svc --dir C:\r --db C:\r.db --http 127.0.0.1:7781 --agent 127.0.0.1:7777`)
	if flag(t, both, "dir") != `C:\r` || flag(t, both, "atrium-dir") != `C:\h` {
		t.Errorf("atrium run mixed up --dir %q and --atrium-dir %q", flag(t, both, "dir"), flag(t, both, "atrium-dir"))
	}

	for line, path := range map[string]string{
		`room --dir C:\Users\claude\.atrium2\room --db C:\Users\claude\.atrium\atrium.db --http 127.0.0.1:7781 --agent 127.0.0.1:7777`: "atrium room",
		`room join TOKEN --identity C:\s.json --dir C:\r --db C:\r.db`:                                                                 "atrium room join",
		`rooms add sgg --transport ziti --service atrium-hub --atrium-dir C:\h`:                                                        "atrium rooms add",
		`rooms ls --atrium-dir C:\h`:        "atrium rooms ls",
		`rooms token sgg`:                   "atrium rooms token",
		`rooms mark sgg --undo`:             "atrium rooms mark",
		`rooms rm sgg --force`:              "atrium rooms rm",
		`rooms log sgg`:                     "atrium rooms log",
		`backups --atrium-dir C:\h`:         "atrium backups",
		`backups restore C:\h\backups\x.db`: "atrium backups restore",
		`db compact --in a.db --out b.db`:   "atrium db compact",
		`ledger --db C:\r.db`:               "atrium ledger",
		`version --short`:                   "atrium version",
	} {
		if got := parses(t, root, line).CommandPath(); got != path {
			t.Errorf("%q ran `%s`, want `%s`", line, got, path)
		}
	}
}

// THE COLLISIONS, resolved. `atrium join` stays the session command the
// /atrium-join skill runs, `atrium hub` is gone, and in the shim `join` is a
// room's first join again.
func TestTheCollidingNamesLandWhereThePlanSays(t *testing.T) {
	root := newRoot()
	join, _, _ := root.Find([]string{"join"})
	if join.Name() != "join" || strings.Contains(join.Use, "join string") {
		t.Errorf("`atrium join` is %q, want the session command", join.Use)
	}
	if hub, _, _ := root.Find([]string{"hub"}); hub != root {
		t.Errorf("`atrium hub` still exists as %q", hub.CommandPath())
	}
	if f := parses(t, root, "room").Flags().Lookup("hub"); f != nil {
		t.Error("`atrium room` still takes the v1 --hub flag")
	}

	shim := asShim(t)
	sjoin, _, _ := shim.Find([]string{"join"})
	if !strings.Contains(sjoin.Use, "join string") {
		t.Errorf("`atrium2 join` is %q, want the room's first join", sjoin.Use)
	}
}

// Every hook line in this machine's settings.json, character for character,
// resolves to the same subcommand with every flag declared, in the one binary
// and in the shim. The shim matters because the room still runs atrium2.exe
// until the cutover, and its address file names it.
func TestTheSettingsHookLinesRunOnBothBinaries(t *testing.T) {
	lines := []string{
		"hook --event tool-start", "hook --event tool-end", "hook --event tool-failed",
		"hook --event prompt", "hook --event notification", "hook --event subagent-start",
		"hook --event subagent-end", "session --event start", "session --event end",
		"session --event compact", "turn --event end",
	}
	t.Setenv("ATRIUM_PERM_GATE", "off")
	t.Setenv("ATRIUM_HUB_URL", "http://127.0.0.1:1")
	t.Setenv("ATRIUM_LOCATION", filepath.Join(t.TempDir(), "none.json"))
	for _, root := range []func() *cobra.Command{newRoot, func() *cobra.Command { return asShim(t) }} {
		for _, line := range lines {
			args := strings.Fields(line)
			cmd, rest, err := root().Find(args)
			if err != nil || cmd.Name() != args[0] || !isRunnerHook(cmd) {
				t.Fatalf("%q resolved to %v (%v)", line, cmd.CommandPath(), err)
			}
			if err := cmd.ParseFlags(rest); err != nil {
				t.Errorf("`%s` does not parse %q: %v", cmd.CommandPath(), line, err)
			}
			restore := withStdin(t, `{}`)
			if code := runRoot(root(), args); code != 0 {
				t.Errorf("%q exited %d on `%s`", line, code, root().Name())
			}
			restore()
		}
	}
}

// The shim keeps atrium2's defaults and address file, the one binary takes
// this machine's layout and the machine's one address file.
func TestTheDefaultsDifferOnlyInTheShim(t *testing.T) {
	now := map[string]string{
		"board": defaultBoardAddr(), "link": defaultLinkAddr(),
		"http": defaultRoomHTTP(), "agent": defaultRoomAgent(),
	}
	want := map[string]string{
		"board": "127.0.0.1:7778", "link": "127.0.0.1:7779", "http": "127.0.0.1:7781", "agent": "127.0.0.1:7777",
	}
	for k, v := range want {
		if now[k] != v {
			t.Errorf("the one binary's %s default is %q, want %q", k, now[k], v)
		}
	}
	def, err := daemon.DefaultLocationPath()
	if err != nil {
		t.Fatal(err)
	}
	if roomLocation() != def {
		t.Errorf("the one binary's room records itself at %q, want the machine's %q", roomLocation(), def)
	}
	if defaultRoomDB() != daemon.DefaultDBPath() {
		t.Errorf("the one binary's room database is %q, want %q", defaultRoomDB(), daemon.DefaultDBPath())
	}

	asShim(t)
	old := map[string]string{
		"board": ":7800", "link": ":7801", "http": "127.0.0.1:7810", "agent": "127.0.0.1:7811",
	}
	got := map[string]string{
		"board": defaultBoardAddr(), "link": defaultLinkAddr(),
		"http": defaultRoomHTTP(), "agent": defaultRoomAgent(),
	}
	for k, v := range old {
		if got[k] != v {
			t.Errorf("the shim's %s default is %q, want atrium2's %q", k, got[k], v)
		}
	}
	if !strings.HasSuffix(filepath.ToSlash(roomLocation()), "atrium2/room/daemon.json") {
		t.Errorf("the shim's room records itself at %q, want atrium2's file", roomLocation())
	}
	if filepath.Base(defaultRoomDB()) != "atrium2.db" {
		t.Errorf("the shim's room database is %q, want atrium2's", defaultRoomDB())
	}
}

// A hint names the command the binary it came from has.
func TestHintsNameTheBinarysOwnCommands(t *testing.T) {
	for sub, want := range map[string]string{
		"rooms add": "atrium rooms add", "room join": "atrium room join", "backups restore": "atrium backups restore",
	} {
		if got := atriumCmd(sub); got != want {
			t.Errorf("atriumCmd(%q) = %q, want %q", sub, got, want)
		}
	}
	asShim(t)
	for sub, want := range map[string]string{
		"rooms add": "atrium2 hub room add", "rooms ls": "atrium2 hub room ls", "room join": "atrium2 join",
		"backups": "atrium2 hub backups", "backups restore": "atrium2 hub restore", "run": "atrium2 hub",
		"room": "atrium2 room",
	} {
		if got := atriumCmd(sub); got != want {
			t.Errorf("shim atriumCmd(%q) = %q, want %q", sub, got, want)
		}
	}
}

// `atrium run` leaves a room that answers alone, and does not mistake a file
// a killed room left behind for one that is up.
func TestRunLeavesAnAnsweringRoomAlone(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer up.Close()
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	if err := os.WriteFile(live, []byte(`{"board":"`+up.URL+`","agent":"http://127.0.0.1:1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := roomAnswers(live); !ok {
		t.Error("a room whose board answers health read as down")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dead := "http://" + ln.Addr().String()
	ln.Close()
	stale := filepath.Join(dir, "stale.json")
	if err := os.WriteFile(stale, []byte(`{"board":"`+dead+`","pid":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := roomAnswers(stale); ok {
		t.Error("a file naming a closed port read as a running room")
	}
	if _, ok := roomAnswers(filepath.Join(dir, "missing.json")); ok {
		t.Error("no address file read as a running room")
	}

	t.Setenv("ATRIUM_LOCATION", live)
	if got := roomAddressFile(roomLaunch{dir: dir}); got != live {
		t.Errorf("run looked for the room at %q, want the inherited %q", got, live)
	}
	t.Setenv("ATRIUM_LOCATION", "")
	if got := roomAddressFile(roomLaunch{dir: dir, isolated: true}); got != filepath.Join(dir, "daemon.json") {
		t.Errorf("an isolated run looked for the room at %q", got)
	}
}

// THE ONE-MACHINE CASE NEEDS NO JOIN STRING. The first `atrium run` names a
// room after the machine, mints it a secret, and enrols it over its own link,
// leaving keys a room can start from. A second run reuses the row.
func TestRunMakesThisMachinesRoomOverItsOwnLink(t *testing.T) {
	hubKeys := link.Keys{Dir: filepath.Join(t.TempDir(), "hub")}
	store, err := hubstore.Open(filepath.Join(hubKeys.Dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := free.Addr().String()
	free.Close()

	side, err := openHub("direct", hubKeys, addr, "", "", func(secret string) (string, error) {
		r, err := store.Spend(secret)
		if err != nil {
			return "", err
		}
		return r.Name, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := side.listen()
	if err != nil {
		t.Fatal(err)
	}
	h := link.NewHub(link.Timings{})
	h.Enrol = side.enrol
	h.Authenticated = side.auth
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = h.Serve(ctx, ln) }()

	if err := mintOwnRoom(store, side, "ziti", link.Keys{Dir: t.TempDir()}); err == nil {
		t.Error("a ziti link made a room on its own")
	}

	roomKeys := link.Keys{Dir: filepath.Join(t.TempDir(), "room")}
	if err := mintOwnRoom(store, side, "direct", roomKeys); err != nil {
		t.Fatalf("making this machine's room: %v", err)
	}
	saved, err := roomKeys.Joined()
	if err != nil {
		t.Fatalf("the room has no saved join after minting: %v", err)
	}
	if saved.Room != defaultRoomName() {
		t.Errorf("the room is called %q, want this machine's name %q", saved.Room, defaultRoomName())
	}
	if saved.Hub != side.says {
		t.Errorf("the room dials %q, want this atrium's link %q", saved.Hub, side.says)
	}

	again := link.Keys{Dir: filepath.Join(t.TempDir(), "room2")}
	if err := mintOwnRoom(store, side, "direct", again); err != nil {
		t.Fatalf("a second mint for the same machine: %v", err)
	}
	rooms, err := store.Rooms()
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 {
		t.Errorf("two mints for one machine made %d rooms, want 1", len(rooms))
	}
}
