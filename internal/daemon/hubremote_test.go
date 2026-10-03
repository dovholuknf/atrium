package daemon

import (
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

// launchDump launches the env-dump helper as a runner and answers its environment, upper-cased by name.
func launchDump(t *testing.T, d *Daemon, req LaunchRequest) map[string]string {
	t.Helper()
	t.Setenv("ATRIUM_TEST_ENV_DUMP", "1")
	if _, err := d.st.SaveHarness(store.Harness{
		ID: "envdump", Label: "env dump", Enabled: true, LaunchMode: store.LaunchPTY,
		Cmd: os.Args[0], Args: []string{"-test.run=^TestHelperEnvDump$"}, Env: req.Env,
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	req.Harness, req.Cwd, req.SpawnedBy, req.Env = "envdump", dir, store.HumanLauncher, nil
	_, _ = d.Launch(req)
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, err := os.ReadFile(filepath.Join(dir, "env.txt"))
		if err == nil {
			seen := map[string]string{}
			for _, kv := range strings.Split(string(raw), "\n") {
				if i := strings.Index(kv, "="); i > 0 {
					seen[strings.ToUpper(kv[:i])] = kv[i+1:]
				}
			}
			return seen
		}
		if time.Now().After(deadline) {
			t.Fatalf("the runner never wrote its environment: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func gitEnvOf(seen map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range seen {
		if strings.HasPrefix(k, "GIT_CONFIG_") {
			out[k] = v
		}
	}
	return out
}

func TestACardsLaunchEnvCarriesItsGitTokenScopedToTheForwarder(t *testing.T) {
	d := testDaemon(t)
	d.setAgentAddr(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7777})
	// A room started from inside another card: its token must not be passed on to this one.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "http.http://127.0.0.1:7777/git/.extraHeader")
	t.Setenv("GIT_CONFIG_VALUE_0", gitsync.HeaderCardToken+": other-card.deadbeef")

	seen := launchDump(t, d, LaunchRequest{})
	id := seen["ATRIUM_TASK_ID"]
	g := gitEnvOf(seen)
	if g["GIT_CONFIG_COUNT"] != "1" ||
		g["GIT_CONFIG_KEY_0"] != "http.http://127.0.0.1:7777/git/.extraHeader" {
		t.Fatalf("the card's git env: %v", g)
	}
	v := g["GIT_CONFIG_VALUE_0"]
	if !strings.HasPrefix(v, gitsync.HeaderCardToken+": "+id+".") || strings.Contains(v, "other-card") {
		t.Fatalf("the header is %q, want this card's token (%s)", v, id)
	}
	// never bare, never in a repository's config or the card's args
	for k, val := range g {
		if strings.HasPrefix(k, "GIT_CONFIG_KEY_") && (val == "http.extraHeader" || strings.HasSuffix(val, ".extraHeader") && !strings.Contains(val, "127.0.0.1:7777/git/")) {
			t.Fatalf("an unscoped header key: %s=%s", k, val)
		}
	}
	// The helper exits at once, and a card that exits loses its token: the room forgets it and the forwarder
	// stops accepting it. (Revoked when the card exits.)
	tok := strings.TrimPrefix(v, gitsync.HeaderCardToken+": ")
	deadline := time.Now().Add(10 * time.Second)
	for {
		d.hubGit.mu.Lock()
		_, held := d.hubGit.issued[id]
		d.hubGit.mu.Unlock()
		if !held {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the card exited and the room still holds its git token")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := d.hubGit.cards.Check(tok); ok {
		t.Fatal("an exited card's token is still accepted by the forwarder")
	}
	// the room never keeps a copy where the board shows card details
	if task, err := d.st.Get(id); err != nil || strings.Contains(task.Why+task.Title, tok) {
		t.Fatalf("the token leaked into the card: %v", err)
	}
}

func TestACardThatRunsOutsideCodeGetsNoGitTokenInItsEnv(t *testing.T) {
	d := testDaemon(t)
	d.setAgentAddr(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7777})
	for name, req := range map[string]LaunchRequest{
		"flag": {OutsideCode: true},
		"tag":  {Tags: []string{OutsideCodeTag}},
	} {
		seen := launchDump(t, d, req)
		if g := gitEnvOf(seen); len(g) != 0 {
			t.Errorf("%s: a card that runs outside code has a git env: %v", name, g)
		}
		d.hubGit.mu.Lock()
		n := len(d.hubGit.issued)
		d.hubGit.mu.Unlock()
		if n != 0 {
			t.Errorf("%s: the room minted a token for it", name)
		}
	}
}

func TestAHarnessGitConfigIsContinuedNotReplaced(t *testing.T) {
	d := testDaemon(t)
	d.setAgentAddr(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7777})
	seen := launchDump(t, d, LaunchRequest{Env: map[string]string{
		"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "user.name", "GIT_CONFIG_VALUE_0": "Worker"}})
	g := gitEnvOf(seen)
	if g["GIT_CONFIG_COUNT"] != "2" || g["GIT_CONFIG_KEY_0"] != "user.name" || g["GIT_CONFIG_VALUE_0"] != "Worker" ||
		!strings.HasSuffix(g["GIT_CONFIG_KEY_1"], "/git/.extraHeader") {
		t.Fatalf("the git env: %v", g)
	}
}

// A PR's tests and `prove` run from the PR runner, which builds its environment from the room's own with atrium's
// taint filter. A card's token in the room's environment (a room started from inside a card) must not be in it.
func TestAPRRunnersCommandsAndForksHaveNoGitTokenInTheirEnv(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "http.http://127.0.0.1:7777/git/.extraHeader")
	t.Setenv("GIT_CONFIG_VALUE_0", gitsync.HeaderCardToken+": C1.abc")
	r := newPRRunner(nil, func(string) {})
	for _, env := range [][]string{
		childEnvFrom(r.baseEnv(), nil, map[string]string{"GIT_TERMINAL_PROMPT": "0"}),
		childEnvFrom(os.Environ(), nil, map[string]string{"ATRIUM_PERM_GATE": "off"}),
	} {
		for _, kv := range env {
			if strings.HasPrefix(strings.ToUpper(kv), "GIT_CONFIG_") || strings.Contains(kv, gitsync.HeaderCardToken) {
				t.Fatalf("a PR runner's environment carries %q", kv)
			}
		}
	}
}

func TestGitPushIsRefusedWhenTheRoomOrTheCardSaysNo(t *testing.T) {
	d := testDaemon(t)
	d.setAgentAddr(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7777})
	task := plainTask(t, d, "pusher")
	if _, err := d.GitPush("no-such-card", "fix/x"); err == nil {
		t.Fatal("pushed for a card that is not there")
	}
	// Launched with no token on this room: refused with the reason.
	if _, err := d.GitPush(task.ID, "fix/x"); err == nil || !strings.Contains(err.Error(), "holds no git token") {
		t.Fatalf("no token: %v", err)
	}
	if err := d.st.SetSetting(store.SettingGitPush, "none"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GitPush(task.ID, "fix/x"); err == nil || !strings.Contains(err.Error(), "git.push is none") {
		t.Fatalf("git.push none: %v", err)
	}
	if err := d.st.SetSetting(store.SettingGitPush, "hub"); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetTags(task.ID, []string{OutsideCodeTag}); err != nil {
		t.Skipf("no way to tag a card here: %v", err)
	}
	if _, err := d.GitPush(task.ID, "fix/x"); err == nil || !strings.Contains(err.Error(), "not the room's own") {
		t.Fatalf("outside code: %v", err)
	}
	if _, err := d.GitPush(task.ID, "+fix/x"); err == nil {
		t.Fatal("a + refspec was taken")
	}
}

func TestACardLosesItsTokenWhenItIsNoLongerRunning(t *testing.T) {
	d := testDaemon(t)
	task := plainTask(t, d, "finisher")
	tok, err := d.hubGit.cards.Mint(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := d.hubGit.cards.Check(tok); !ok || id != task.ID {
		t.Fatal("a running card's token was refused")
	}
	if err := d.st.SetStatus(task.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.hubGit.cards.Check(tok); ok {
		t.Fatal("a dead card's token was accepted")
	}
	d.revokeHubGit(task.ID, time.Time{})
}

func TestTheForwarderRouteIsOnTheAgentListenerAndTheTokenOpensNothingElse(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	base := "http://" + d.opts.AgentAddr
	task := plainTask(t, d, "walker")
	tok, _ := d.hubGit.cards.Mint(task.ID)
	do := func(path string, withToken bool) (int, string) {
		req, _ := http.NewRequest("GET", base+path, nil)
		if withToken {
			req.Header.Set(gitsync.HeaderCardToken, tok)
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	// The route is mounted: with a good token and no hub it says so, in the forwarder's own words, and with no
	// token it says that instead.
	code, body := do("/git/hub/github/o/r.git/info/refs?service=git-upload-pack", true)
	if code != 200 || !strings.Contains(body, "not attached to a hub") {
		t.Fatalf("the forwarder route with a token: %d %q", code, body)
	}
	if code, body := do("/git/hub/github/o/r.git/info/refs?service=git-upload-pack", false); code != 200 || !strings.Contains(body, "atrium token") {
		t.Fatalf("the forwarder route with none: %d %q", code, body)
	}
	// The token is not a credential for anything else on the agent listener.
	for _, p := range []string{"/peers", "/hooks-changed", "/session", "/permission"} {
		before, _ := do(p, false)
		after, _ := do(p, true)
		if before != after {
			t.Errorf("%s answers %d without the token and %d with it", p, before, after)
		}
	}
}

// A GIT_CONFIG_* an inherited environment carries (a room started from inside a card) is not passed to a runner.
func TestInheritedGitConfigIsTaintedForLaunchedRunners(t *testing.T) {
	for _, k := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_CONFIG_VALUE_12", "git_config_value_1"} {
		if !inheritedTaint(k) {
			t.Errorf("%s is passed to launched runners", k)
		}
	}
	// Only those: the operator's own git settings elsewhere in GIT_* are not this filter's business.
	if inheritedTaint("GIT_CONFIG_GLOBAL") {
		t.Error("GIT_CONFIG_GLOBAL was tainted too")
	}
}

// A resume mints its token before it registers the new runner, so the old run's late exit must not take it away.
func TestAnOldRunsExitDoesNotRevokeTheTokenOfAResume(t *testing.T) {
	d := testDaemon(t)
	d.setAgentAddr(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7777})
	task := plainTask(t, d, "resumer")
	oldStarted := time.Now()
	time.Sleep(5 * time.Millisecond)
	into := map[string]string{}
	d.hubGitEnv(task.ID, false, nil, into) // the resume's launch
	tok := strings.TrimPrefix(into["GIT_CONFIG_VALUE_0"], gitsync.HeaderCardToken+": ")
	d.revokeHubGit(task.ID, oldStarted) // the old runner's awaitExit
	if id, ok := d.hubGit.cards.Check(tok); !ok || id != task.ID {
		t.Fatal("the old run's exit revoked the resumed card's new token")
	}
	// The new run's own exit does revoke it.
	d.revokeHubGit(task.ID, time.Now())
	if _, ok := d.hubGit.cards.Check(tok); ok {
		t.Fatal("the token survived its own run's exit")
	}
}

// A listener bound to one address is reached at that address. Only a wildcard bind is reached at loopback.
func TestAgentAddrIsKeptForASpecificBindAndMappedForAWildcard(t *testing.T) {
	d := testDaemon(t)
	for _, c := range []struct {
		in   net.Addr
		want string
	}{
		{&net.TCPAddr{IP: net.ParseIP("192.168.1.7"), Port: 7777}, "192.168.1.7:7777"},
		{&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 7777}, "127.0.0.1:7777"},
		{&net.TCPAddr{IP: net.IPv4zero, Port: 7777}, "127.0.0.1:7777"},
		{&net.TCPAddr{IP: net.IPv6unspecified, Port: 7777}, "127.0.0.1:7777"},
	} {
		d.setAgentAddr(c.in)
		if got := d.agentAddr(); got != c.want {
			t.Errorf("%v: %s, want %s", c.in, got, c.want)
		}
	}
}
