//go:build integration

package gitsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type who struct {
	caller string
	card   string
	chain  string
}

var operator = who{caller: "operator"}

func roomCard(room, card string, chain ...string) who {
	w := who{caller: "room:" + room, card: card}
	if len(chain) > 0 {
		w.chain = strings.Join(chain, ",")
	} else {
		w.chain = card
	}
	return w
}

type cardAns struct {
	st  CardState
	err error
}

type recvFix struct {
	t    *testing.T
	h    *Hub
	log  *MemPushLog
	srv  *httptest.Server
	work string

	mu    sync.Mutex
	cards map[string]cardAns
	asked atomic.Int32

	create atomic.Bool
}

// newRecv is a hub with a store and a server in front of it. `set` changes the hub BEFORE the server starts: a field
// written once requests are being served is a race the detector sees, because the client is a git process and gives the
// test goroutine no ordering with the handlers.
func newRecv(t *testing.T, set ...func(*Hub)) *recvFix {
	t.Helper()
	root := t.TempDir()
	x := &recvFix{t: t, log: &MemPushLog{}, cards: map[string]cardAns{}}
	x.h = &Hub{Dir: filepath.Join(root, "hub"), Runner: NewRunner(), PushLog: x.log}
	for _, f := range set {
		f(x.h)
	}
	x.h.Cards = func(_ context.Context, room, card string) (CardState, error) {
		x.asked.Add(1)
		x.mu.Lock()
		defer x.mu.Unlock()
		if a, ok := x.cards[room+"/"+card]; ok {
			return a.st, a.err
		}
		return CardState{Status: "working"}, nil
	}
	x.h.CreateOnPush = x.create.Load
	handler := x.h.StoreHandler()
	x.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		switch c := r.Header.Get("X-Test-Caller"); {
		case c == "operator":
			ctx = WithCaller(ctx, Caller{Kind: CallerOperator})
		case strings.HasPrefix(c, "room:"):
			ctx = WithCaller(ctx, Caller{Kind: CallerRoom, Room: strings.TrimPrefix(c, "room:")})
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(x.srv.Close)

	x.work = filepath.Join(root, "work")
	if err := os.MkdirAll(x.work, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, x.work, "init", "-q", "-b", "main")
	commit(t, x.work, "base.txt", "base")
	return x
}

func (x *recvFix) setCard(room, card string, st CardState, err error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.cards[room+"/"+card] = cardAns{st, err}
}

func (x *recvFix) url(name string) string { return x.srv.URL + StorePrefix + name + ".git" }

// makeRepo puts an empty repository in the store, as `init` would for a private one.
func (x *recvFix) makeRepo(name string) string {
	x.t.Helper()
	ref, err := ParseName(name)
	if err != nil {
		x.t.Fatal(err)
	}
	dir := x.h.Store().dir(ref)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		x.t.Fatal(err)
	}
	if err := x.h.Store().create(bg, dir); err != nil {
		x.t.Fatal(err)
	}
	return dir
}

// run is git in dir, acting as `w`. The text is stdout and stderr on a failure.
func (x *recvFix) run(dir string, w who, args ...string) (string, error) {
	x.t.Helper()
	full := []string{}
	if w.caller != "" {
		full = append(full, "-c", "http.extraHeader=X-Test-Caller: "+w.caller)
	}
	if w.card != "" {
		full = append(full, "-c", "http.extraHeader="+HeaderCard+": "+w.card)
	}
	if w.chain != "" {
		full = append(full, "-c", "http.extraHeader="+HeaderChain+": "+w.chain)
	}
	out, err := Default.GitEnv(bg, dir, []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"},
		append(full, args...)...)
	var ge *Error
	if errors.As(err, &ge) {
		return out + ge.Stderr, err
	}
	return out, err
}

// branch makes a commit on a new branch of the work repository, off main, and answers its sha.
func (x *recvFix) branch(name, file string) string {
	x.t.Helper()
	git(x.t, x.work, "switch", "-q", "-C", name, "main")
	sha := commit(x.t, x.work, file, name+file)
	git(x.t, x.work, "switch", "-q", "main")
	return sha
}

// grow adds a commit to a branch of the work repository.
func (x *recvFix) grow(name, file string) string {
	x.t.Helper()
	git(x.t, x.work, "switch", "-q", name)
	sha := commit(x.t, x.work, file, name+file)
	git(x.t, x.work, "switch", "-q", "main")
	return sha
}

func (x *recvFix) push(w who, name string, refspecs ...string) (string, error) {
	x.t.Helper()
	return x.run(x.work, w, append([]string{"push", x.url(name)}, refspecs...)...)
}

func (x *recvFix) must(out string, err error) string {
	x.t.Helper()
	if err != nil {
		x.t.Fatalf("%v\n%s", err, out)
	}
	return out
}

// refOn is a ref's sha in the store's repository, or "".
func (x *recvFix) refOn(name, ref string) string {
	x.t.Helper()
	dir, ok := x.h.Store().Path(name)
	if ok != nil {
		x.t.Fatal(ok)
	}
	out, err := Default.Git(bg, dir, "for-each-ref", "--format=%(objectname)", ref)
	if err != nil {
		x.t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

// refsOn is every branch and tag in the store's repository.
func (x *recvFix) refsOn(name string) string {
	x.t.Helper()
	dir, _ := x.h.Store().Path(name)
	out, err := Default.Git(bg, dir, "for-each-ref", "--format=%(refname) %(objectname)")
	if err != nil {
		x.t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

// noLeftovers fails when a quarantine directory is still in the repository's objects.
func (x *recvFix) noLeftovers(name string) {
	x.t.Helper()
	dir, _ := x.h.Store().Path(name)
	ents, _ := os.ReadDir(filepath.Join(dir, "objects"))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "incoming") || strings.HasPrefix(e.Name(), "tmp_objdir") {
			x.t.Errorf("a quarantine directory %q was left in the repository", e.Name())
		}
	}
}

// seedMain makes the operator push main, as the operator does to a repository `init` made empty.
func (x *recvFix) seedMain(name string) string {
	x.t.Helper()
	x.makeRepo(name)
	x.must(x.push(operator, name, "main:refs/heads/main"))
	return x.refOn(name, MainRef)
}

// rawPush sends a receive-pack request by hand: the commands, a flush and an empty pack.
func (x *recvFix) rawPush(w who, name string, updates ...Update) (int, string) {
	x.t.Helper()
	var body bytes.Buffer
	for i, u := range updates {
		line := u.Old + " " + u.New + " " + u.Ref
		if i == 0 {
			line += "\x00report-status side-band-64k agent=test"
		}
		body.Write(pktLine(line))
	}
	body.Write(flushPkt)
	body.WriteString("PACK\x00\x00\x00\x02\x00\x00\x00\x00")
	return x.post(w, name, "git-receive-pack", "application/x-git-receive-pack-request", body.Bytes())
}

func (x *recvFix) post(w who, name, svc, ctype string, body []byte) (int, string) {
	x.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, x.url(name)+"/"+svc, bytes.NewReader(body))
	req.Header.Set("Content-Type", ctype)
	x.stamp(req, w)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		x.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func (x *recvFix) stamp(req *http.Request, w who) {
	if w.caller != "" {
		req.Header.Set("X-Test-Caller", w.caller)
	}
	if w.card != "" {
		req.Header.Set(HeaderCard, w.card)
	}
	if w.chain != "" {
		req.Header.Set(HeaderChain, w.chain)
	}
}

func (x *recvFix) getRefs(w who, name, service string, extra ...string) (int, string) {
	x.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, x.url(name)+"/info/refs?service="+service, nil)
	x.stamp(req, w)
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header.Set(extra[i], extra[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		x.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func (x *recvFix) rows() []PushRow { return x.log.Rows() }

// pushRows is the rows of the log that are pushes.
func (x *recvFix) pushRows() []PushRow {
	var out []PushRow
	for _, r := range x.rows() {
		if !r.Release {
			out = append(out, r)
		}
	}
	return out
}

const hubRepo = "github/o/r"

func TestACardsPushOfANewBranchLandsAndIsLogged(t *testing.T) {
	x := newRecv(t)
	main := x.seedMain(hubRepo)
	sha := x.branch("fix/x", "x.txt")
	out, err := x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x")
	x.must(out, err)

	if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != sha {
		t.Fatalf("fix/x is %q on the hub, want %q", got, sha)
	}
	if got := x.refOn(hubRepo, MainRef); got != main {
		t.Fatalf("main moved to %s", got)
	}
	rows := x.pushRows()
	if len(rows) != 2 {
		t.Fatalf("rows: %+v", rows)
	}
	r := rows[1]
	if r.Repo != hubRepo || r.Ref != "refs/heads/fix/x" || r.New != sha || r.Old != zeroSHA ||
		r.Room != "sg4" || r.Card != "C1" || r.At.IsZero() || r.ReleasedBy != "" {
		t.Fatalf("the row: %+v", r)
	}
	if rows[0].Room != "" || rows[0].Card != "" || !rows[0].Operator() {
		t.Fatalf("the operator's row has a room or card: %+v", rows[0])
	}
	x.noLeftovers(hubRepo)
}

func TestTheOwnerFastForwardsItsOwnBranch(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	card := roomCard("sg4", "C1")
	x.must(x.push(card, hubRepo, "fix/x:refs/heads/fix/x"))
	sha2 := x.grow("fix/x", "y.txt")
	x.must(x.push(card, hubRepo, "fix/x:refs/heads/fix/x"))
	if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != sha2 {
		t.Fatalf("fix/x is %s, want %s", got, sha2)
	}
	if o, c, ok, _ := x.log.Owner(bg, hubRepo, "refs/heads/fix/x"); !ok || o != "sg4" || c != "C1" {
		t.Fatalf("owner %s %s %v", o, c, ok)
	}
}

func TestANonFastForwardIsRefusedAndMovesNothing(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	first := x.branch("fix/x", "x.txt")
	card := roomCard("sg4", "C1")
	x.must(x.push(card, hubRepo, "fix/x:refs/heads/fix/x"))

	// A different history for the same name, pushed with --force: the hub's old matches, the new is not a
	// descendant, so it is the hook that refuses, after the pack is in quarantine.
	other := x.branch("fix/other", "z.txt")
	out, err := x.push(card, hubRepo, "--force", "fix/other:refs/heads/fix/x")
	if err == nil {
		t.Fatalf("a non-fast-forward was taken:\n%s", out)
	}
	if !strings.Contains(out, "not a fast-forward") && !strings.Contains(out, "rejected") {
		t.Fatalf("the refusal does not say why:\n%s", out)
	}
	if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != first {
		t.Fatalf("fix/x moved to %s (was %s, the refused push had %s)", got, first, other)
	}
	if n := len(x.pushRows()); n != 2 {
		t.Fatalf("a refused push was logged: %d rows", n)
	}
	x.noLeftovers(hubRepo)
}

func TestARefusalReachesGitAsRemoteRejectedWithTheSentence(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	out, err := x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/main")
	if err == nil {
		t.Fatalf("a card moved main:\n%s", out)
	}
	for _, want := range []string{"remote: atrium: ", "only the operator moves main", "[remote rejected]", "nothing was pushed"} {
		if !strings.Contains(out, want) {
			t.Errorf("git's output has no %q:\n%s", want, out)
		}
	}
}

func TestAPushOfMainFromACardIsRefusedAndFromTheOperatorLands(t *testing.T) {
	x := newRecv(t)
	main := x.seedMain(hubRepo)
	for _, ref := range []string{"refs/heads/main", "refs/heads/claude/main"} {
		x.branch("fix/m", "m.txt")
		if out, err := x.push(roomCard("sg4", "C1"), hubRepo, "fix/m:"+ref); err == nil {
			t.Fatalf("a card pushed %s:\n%s", ref, out)
		}
	}
	if got := x.refOn(hubRepo, MainRef); got != main {
		t.Fatalf("main moved to %s", got)
	}
	if got := x.refOn(hubRepo, ClaudeMainRef); got != "" {
		t.Fatalf("claude/main appeared: %s", got)
	}

	// The operator fast-forwards it.
	git(t, x.work, "switch", "-q", "main")
	sha := commit(t, x.work, "next.txt", "next")
	x.must(x.push(operator, hubRepo, "main:refs/heads/main"))
	if got := x.refOn(hubRepo, MainRef); got != sha {
		t.Fatalf("the operator's push did not land: %s", got)
	}
	// And only fast-forwards: a rewrite of main is refused.
	git(t, x.work, "reset", "-q", "--hard", main)
	diverged := commit(t, x.work, "other.txt", "other")
	if out, err := x.push(operator, hubRepo, "--force", "main:refs/heads/main"); err == nil {
		t.Fatalf("the operator rewrote main with %s:\n%s", diverged, out)
	}
	if got := x.refOn(hubRepo, MainRef); got != sha {
		t.Fatalf("main is %s after a refused rewrite, want %s", got, sha)
	}
	x.noLeftovers(hubRepo)
}

func TestAnotherCardCannotPushToAnOwnedBranchOnAnyRoom(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	owned := x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	sha2 := x.grow("fix/x", "more.txt")

	for _, w := range []who{roomCard("sg4", "C2"), roomCard("m1mini", "C1"), roomCard("m1mini", "C9")} {
		out, err := x.push(w, hubRepo, "fix/x:refs/heads/fix/x")
		if err == nil {
			t.Fatalf("%v pushed to a branch sg4's C1 owns:\n%s", w, out)
		}
		if !strings.Contains(out, "owned by sg4's C1, fetch it and push under another name") {
			t.Fatalf("the refusal does not name the owner:\n%s", out)
		}
		if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != owned {
			t.Fatalf("fix/x moved to %s (the refused push had %s)", got, sha2)
		}
	}
	if len(x.pushRows()) != 2 {
		t.Fatalf("rows: %+v", x.pushRows())
	}
}

func TestTheOwnersSuccessorByChainPushesAndTakesTheBranchOver(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	sha2 := x.grow("fix/x", "more.txt")

	// C2 is C1's successor on the same room, and says so.
	succ := roomCard("sg4", "C2", "C2", "C1")
	x.must(x.push(succ, hubRepo, "fix/x:refs/heads/fix/x"))
	if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != sha2 {
		t.Fatalf("the successor's push did not land: %s", got)
	}
	if o, c, ok, _ := x.log.Owner(bg, hubRepo, "refs/heads/fix/x"); !ok || o != "sg4" || c != "C2" {
		t.Fatalf("the owner is %s %s %v, want sg4 C2", o, c, ok)
	}
	rows := x.pushRows()
	if last := rows[len(rows)-1]; last.Card != "C2" || !strings.Contains(last.ReleasedBy, "moved to C2") {
		t.Fatalf("the row for the takeover: %+v", last)
	}
	// The predecessor is no longer the owner: a push by it, with no chain, is another card's.
	x.grow("fix/x", "again.txt")
	if out, err := x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"); err == nil {
		t.Fatalf("the old owner pushed after it was succeeded:\n%s", out)
	}
}

// A CHAIN IS HONOURED ONLY AGAINST AN OWNER ON THE PUSHER'S OWN ROOM. The hub trusts a room's forwarder for its
// own cards, and no other room's.
func TestAChainNamingAnotherRoomsCardDoesNotHelp(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	x.grow("fix/x", "more.txt")

	forged := roomCard("m1mini", "C7", "C7", "C1")
	out, err := x.push(forged, hubRepo, "fix/x:refs/heads/fix/x")
	if err == nil {
		t.Fatalf("a chain naming sg4's C1 let m1mini's C7 in:\n%s", out)
	}
	if !strings.Contains(out, "owned by sg4's C1") {
		t.Fatalf("the refusal: %s", out)
	}
	// The chain must start with the card, and is capped.
	for name, w := range map[string]who{
		"a chain that does not start with the card": {caller: "room:sg4", card: "C2", chain: "C9,C1"},
		"a chain that is too long":                  {caller: "room:sg4", card: "C2", chain: "C2,a,b,c,d,e,f,g,h"},
		"a bad id in the chain":                     {caller: "room:sg4", card: "C2", chain: "C2,C1/../x"},
	} {
		if out, err := x.push(w, hubRepo, "fix/x:refs/heads/fix/x"); err == nil {
			t.Fatalf("%s was taken:\n%s", name, out)
		}
	}
}

func TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch(t *testing.T) {
	for name, tc := range map[string]struct {
		st   CardState
		err  error
		want string
	}{
		"gone":      {err: ErrCardGone, want: "card gone"},
		"dead":      {st: CardState{Status: "dead"}, want: "card dead"},
		"done 8 d":  {st: CardState{Status: "done", IdleSeconds: 8 * 24 * 3600}, want: "card done over 7 days"},
		"done 365d": {st: CardState{Status: "done", IdleSeconds: 365 * 24 * 3600}, want: "card done over 7 days"},
	} {
		t.Run(name, func(t *testing.T) {
			x := newRecv(t)
			x.seedMain(hubRepo)
			x.branch("fix/x", "x.txt")
			x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
			sha2 := x.grow("fix/x", "more.txt")
			x.setCard("sg4", "C1", tc.st, tc.err)

			x.must(x.push(roomCard("m1mini", "C5"), hubRepo, "fix/x:refs/heads/fix/x"))
			if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != sha2 {
				t.Fatalf("the new owner's fast-forward did not land: %s", got)
			}
			if o, c, ok, _ := x.log.Owner(bg, hubRepo, "refs/heads/fix/x"); !ok || o != "m1mini" || c != "C5" {
				t.Fatalf("owner %s %s %v", o, c, ok)
			}
			rows := x.rows()
			var sawRelease bool
			for _, r := range rows {
				if r.Release && r.ReleasedBy == tc.want && r.Room == "sg4" && r.Card == "C1" {
					sawRelease = true
				}
			}
			if !sawRelease {
				t.Fatalf("no release marker for %q in %+v", tc.want, rows)
			}
			if last := rows[len(rows)-1]; last.ReleasedBy != tc.want || last.Card != "C5" {
				t.Fatalf("the takeover row: %+v", last)
			}
		})
	}
}

func TestAnOwnerWhoIsLiveOrRecentlyDoneKeepsTheBranch(t *testing.T) {
	for name, st := range map[string]CardState{
		"running":  {Status: "working"},
		"idle":     {Status: "needs-input", IdleSeconds: 30 * 24 * 3600},
		"done 6 d": {Status: "done", IdleSeconds: 6 * 24 * 3600},
	} {
		t.Run(name, func(t *testing.T) {
			x := newRecv(t)
			x.seedMain(hubRepo)
			owned := x.branch("fix/x", "x.txt")
			x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
			x.grow("fix/x", "more.txt")
			x.setCard("sg4", "C1", st, nil)
			if out, err := x.push(roomCard("m1mini", "C5"), hubRepo, "fix/x:refs/heads/fix/x"); err == nil {
				t.Fatalf("took a branch whose owner is %+v:\n%s", st, out)
			}
			if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != owned {
				t.Fatalf("fix/x moved")
			}
		})
	}
}

func TestAnUnreachableOwnersRoomKeepsTheBranchOwnedAndSaysWhy(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	x.grow("fix/x", "more.txt")
	x.setCard("sg4", "C1", CardState{}, ErrRoomUnreachable)

	out, err := x.push(roomCard("m1mini", "C5"), hubRepo, "fix/x:refs/heads/fix/x")
	if err == nil {
		t.Fatalf("took a branch whose room cannot be asked:\n%s", out)
	}
	if !strings.Contains(out, "owned by sg4's C1") || !strings.Contains(out, "cannot reach sg4") {
		t.Fatalf("the refusal does not say the room is unreachable:\n%s", out)
	}
	// No lookup wired at all behaves the same.
	x.h.Cards = nil
	if _, err := x.push(roomCard("m1mini", "C5"), hubRepo, "fix/x:refs/heads/fix/x"); err == nil {
		t.Fatal("took a branch with no way to ask")
	}
}

func TestTheOperatorReleasesABranchAndTheNextCardOwnsIt(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	x.grow("fix/x", "more.txt")

	msg, err := x.h.ReleaseBranch(bg, hubRepo, "fix/x")
	if err != nil || !strings.Contains(msg, "sg4's C1") {
		t.Fatalf("release: %q %v", msg, err)
	}
	x.must(x.push(roomCard("m1mini", "C5"), hubRepo, "fix/x:refs/heads/fix/x"))
	if o, c, _, _ := x.log.Owner(bg, hubRepo, "refs/heads/fix/x"); o != "m1mini" || c != "C5" {
		t.Fatalf("owner %s %s", o, c)
	}
	if msg, err := x.h.ReleaseBranch(bg, hubRepo, "never/pushed"); err != nil || !strings.Contains(msg, "no owner") {
		t.Fatalf("releasing an unowned branch: %q %v", msg, err)
	}
	for _, bad := range []string{"main", "claude/main", "../x", "a b"} {
		if _, err := x.h.ReleaseBranch(bg, hubRepo, bad); err == nil {
			t.Errorf("released %q", bad)
		}
	}
	if _, err := x.h.ReleaseBranch(bg, "github/no/such", "x"); err == nil {
		t.Error("released a branch of a repository the hub lacks")
	}
}

func TestTheOperatorMayFastForwardAnyBranchAndOwnershipIsUnchanged(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	sha2 := x.grow("fix/x", "more.txt")
	x.must(x.push(operator, hubRepo, "fix/x:refs/heads/fix/x"))
	if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != sha2 {
		t.Fatalf("the operator's push did not land: %s", got)
	}
	if o, c, ok, _ := x.log.Owner(bg, hubRepo, "refs/heads/fix/x"); !ok || o != "sg4" || c != "C1" {
		t.Fatalf("an operator push changed the owner to %s %s %v", o, c, ok)
	}
	// A branch the operator makes is the operator's: a card cannot push to it.
	x.branch("integration", "i.txt")
	x.must(x.push(operator, hubRepo, "integration:refs/heads/integration"))
	x.grow("integration", "j.txt")
	out, err := x.push(roomCard("sg4", "C1"), hubRepo, "integration:refs/heads/integration")
	if err == nil || !strings.Contains(out, "owned by the operator") {
		t.Fatalf("a card pushed to the operator's branch: %v\n%s", err, out)
	}
}

func TestACaseVariantOfAnExistingBranchIsRefused(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	first := x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	x.branch("Fix/x", "y.txt")

	for _, w := range []who{roomCard("sg4", "C1"), roomCard("m1mini", "C2"), operator} {
		out, err := x.push(w, hubRepo, "Fix/x:refs/heads/Fix/x")
		if err == nil {
			t.Fatalf("%v pushed Fix/x beside fix/x:\n%s", w, out)
		}
		if !strings.Contains(out, "only in case") {
			t.Fatalf("the refusal does not say why:\n%s", out)
		}
	}
	if refs := x.refsOn(hubRepo); strings.Contains(refs, "Fix/x") || x.refOn(hubRepo, "refs/heads/fix/x") != first {
		t.Fatalf("refs: %s", refs)
	}
	// A directory against a file is the same clash, and refused here rather than half way through.
	if _, out := x.rawPush(roomCard("sg4", "C1"), hubRepo, Update{zeroSHA, first, "refs/heads/fix"}); !strings.Contains(out, "clashes") {
		t.Fatalf("fix beside fix/x: %q", out)
	}
	if _, out := x.rawPush(roomCard("sg4", "C1"), hubRepo, Update{zeroSHA, first, "refs/heads/fix/x/deeper"}); !strings.Contains(out, "clashes") {
		t.Fatalf("fix/x/deeper beside fix/x: %q", out)
	}
	x.noLeftovers(hubRepo)
}

func TestTwoNewRefsInOnePushThatDifferOnlyInCaseAreRefused(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	a := x.branch("a/b", "1.txt")
	_, out := x.rawPush(roomCard("sg4", "C1"), hubRepo,
		Update{zeroSHA, a, "refs/heads/a/b"}, Update{zeroSHA, a, "refs/heads/A/B"})
	if !strings.Contains(out, "ng refs/heads/A/B") || !strings.Contains(out, "only in case") {
		t.Fatalf("the response: %q", out)
	}
	if got := x.refOn(hubRepo, "refs/heads/a/b"); got != "" {
		t.Fatalf("a/b landed: %s", got)
	}
}

func TestAHostileRefNameSet(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	sha := x.branch("fix/x", "x.txt")
	long := "refs/heads/" + strings.Repeat("a", 300)
	for name, ref := range map[string]string{
		"dotdot":             "refs/heads/a/../b",
		"dotdot in a part":   "refs/heads/a..b",
		"backslash":          "refs/heads/a\\b",
		"control char":       "refs/heads/a\x01b",
		"a newline":          "refs/heads/a\nb",
		"a space":            "refs/heads/a b",
		"a tilde":            "refs/heads/a~1",
		"a caret":            "refs/heads/a^",
		"a colon":            "refs/heads/a:b",
		"a question mark":    "refs/heads/a?",
		"a star":             "refs/heads/a*",
		"a bracket":          "refs/heads/a[0]",
		"at brace":           "refs/heads/a@{1}",
		"a bare @":           "refs/heads/@",
		"leading dot part":   "refs/heads/.hidden",
		"leading dot nested": "refs/heads/a/.b",
		"trailing dot":       "refs/heads/a.",
		"trailing slash":     "refs/heads/a/",
		"double slash":       "refs/heads/a//b",
		"lock suffix":        "refs/heads/a.lock",
		"lock nested":        "refs/heads/a.lock/b",
		"empty":              "refs/heads/",
		"too long":           long,
		"unicode":            "refs/heads/r\u00e9ponse",
		"windows device":     "refs/heads/con",
		"windows device ext": "refs/heads/nul.txt",
		"a leading dash":     "refs/heads/-x/y/.",
		"notes":              "refs/notes/x",
		"stash":              "refs/stash",
		"remotes":            "refs/remotes/origin/x",
		"HEAD":               "HEAD",
		"atrium":             "refs/atrium/x",
		"changes":            "refs/changes/1",
		"a bare name":        "fix",
		"heads without refs": "heads/x",
		"tags as a branch":   "refs/tag/x",
	} {
		code, out := x.rawPush(roomCard("sg4", "C1"), hubRepo, Update{zeroSHA, sha, ref})
		if code != 200 || !strings.Contains(out, "ng ") {
			t.Errorf("%s: %q was not refused: %d %q", name, ref, code, out)
		}
		// And not for the operator either, apart from the ones that are plain bad names.
		if strings.HasPrefix(ref, "refs/notes") || ref == "refs/stash" || ref == "HEAD" {
			if _, out := x.rawPush(operator, hubRepo, Update{zeroSHA, sha, ref}); !strings.Contains(out, "ng ") {
				t.Errorf("%s: the operator pushed %q: %q", name, ref, out)
			}
		}
	}
	if refs := x.refsOn(hubRepo); strings.Count(refs, "\n") != 0 {
		t.Fatalf("something landed: %s", refs)
	}
}

func TestDeleteTagNotesAndStashFromACardAreRefusedAndNothingMoves(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	before := x.refsOn(hubRepo)
	card := roomCard("sg4", "C1")

	git(t, x.work, "tag", "v1", "main")
	for name, spec := range map[string]string{
		"a delete":   ":refs/heads/fix/x",
		"a tag":      "refs/tags/v1:refs/tags/v1",
		"notes":      "main:refs/notes/x",
		"stash":      "main:refs/stash",
		"remote ref": "main:refs/remotes/origin/x",
	} {
		out, err := x.push(card, hubRepo, spec)
		if err == nil {
			t.Fatalf("%s was taken:\n%s", name, out)
		}
		if got := x.refsOn(hubRepo); got != before {
			t.Fatalf("%s moved something:\n%s\nwas\n%s", name, got, before)
		}
	}
	// A delete is refused for the operator too, and the operator may push a tag.
	if out, err := x.push(operator, hubRepo, ":refs/heads/fix/x"); err == nil {
		t.Fatalf("the operator deleted a branch by a push:\n%s", out)
	}
	x.must(x.push(operator, hubRepo, "refs/tags/v1:refs/tags/v1"))
	if x.refOn(hubRepo, "refs/tags/v1") == "" {
		t.Fatal("the operator's tag did not land")
	}
	// But a tag is not moved, by anyone.
	git(t, x.work, "tag", "-f", "v1", "fix/x")
	if out, err := x.push(operator, hubRepo, "--force", "refs/tags/v1:refs/tags/v1"); err == nil {
		t.Fatalf("a tag was moved:\n%s", out)
	}
	x.noLeftovers(hubRepo)
}

func TestAMultiRefPushWithOneBadRefMovesNothing(t *testing.T) {
	x := newRecv(t)
	main := x.seedMain(hubRepo)
	x.branch("fix/a", "a.txt")
	x.branch("fix/b", "b.txt")
	before := x.refsOn(hubRepo)

	// One of three is main, which a card may not move: refused by the hub's own rules.
	out, err := x.push(roomCard("sg4", "C1"), hubRepo, "fix/a:refs/heads/fix/a", "fix/b:refs/heads/main", "fix/b:refs/heads/fix/b")
	if err == nil {
		t.Fatalf("a push with main in it was taken:\n%s", out)
	}
	if got := x.refsOn(hubRepo); got != before {
		t.Fatalf("refs moved:\n%s\nwas\n%s", got, before)
	}
	x.noLeftovers(hubRepo)

	// One of two is not a fast-forward, which only the hook can see, with the pack already in quarantine.
	ok := x.branch("fix/ok", "ok.txt")
	x.must(x.push(roomCard("sg4", "C2"), hubRepo, "fix/ok:refs/heads/fix/ok"))
	before = x.refsOn(hubRepo)
	x.branch("fix/new", "n.txt")
	// A rewrite of fix/ok with different content, by --force, alongside a new branch that is fine.
	git(t, x.work, "switch", "-q", "-C", "fix/ok", main)
	commit(t, x.work, "rewrite.txt", "rewrite")
	git(t, x.work, "switch", "-q", "main")
	out, err = x.push(roomCard("sg4", "C2"), hubRepo, "--force", "fix/new:refs/heads/fix/new", "fix/ok:refs/heads/fix/ok")
	if err == nil {
		t.Fatalf("a push with a rewrite in it was taken:\n%s", out)
	}
	if got := x.refsOn(hubRepo); got != before {
		t.Fatalf("a fine ref landed beside a refused one:\n%s\nwas\n%s", got, before)
	}
	if x.refOn(hubRepo, "refs/heads/fix/ok") != ok {
		t.Fatal("fix/ok moved")
	}
	x.noLeftovers(hubRepo)
	if n := len(x.pushRows()); n != 2 {
		t.Fatalf("rows: %+v", x.pushRows())
	}
}

func TestAnOldShaThatIsNotTheRefsCurrentValueIsRefused(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	first := x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	second := x.grow("fix/x", "more.txt")
	stale := "1234567890123456789012345678901234567890"

	card := roomCard("sg4", "C1")
	for name, u := range map[string]Update{
		"a wrong old":          {stale, second, "refs/heads/fix/x"},
		"create over existing": {zeroSHA, second, "refs/heads/fix/x"},
		"update of a missing":  {first, second, "refs/heads/fix/missing"},
	} {
		code, out := x.rawPush(card, hubRepo, u)
		if code != 200 || !strings.Contains(out, "ng "+u.Ref) || !strings.Contains(out, "not where your push expected") {
			t.Errorf("%s: %d %q", name, code, out)
		}
	}
	if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != first {
		t.Fatalf("fix/x moved to %s", got)
	}
}

func TestAPushToARepositoryTheHubLacksIsRefusedUnlessCreateOnPushIsOn(t *testing.T) {
	x := newRecv(t)
	sha := x.branch("fix/x", "x.txt")
	card := roomCard("sg4", "C1")

	out, err := x.push(card, "github/new/thing", "fix/x:refs/heads/fix/x")
	if err == nil {
		t.Fatalf("created a repository with create_on_push off:\n%s", out)
	}
	if !strings.Contains(out, "atrium rooms git init") || !strings.Contains(out, "remote error") {
		t.Fatalf("the refusal does not tell the operator what to run:\n%s", out)
	}
	if x.h.Store().Exists("github/new/thing") {
		t.Fatal("the repository was made")
	}
	// The operator does not create by push either, with it on or off.
	x.create.Store(true)
	if out, err := x.push(operator, "github/new/thing", "fix/x:refs/heads/main"); err == nil || !strings.Contains(out, "rooms git init") {
		t.Fatalf("the operator created by a push: %v\n%s", err, out)
	}

	// On: a card's first push makes it, under the same collision check as init, and logs it.
	x.must(x.push(card, "github/new/thing", "fix/x:refs/heads/fix/x"))
	if !x.h.Store().Exists("github/new/thing") || x.refOn("github/new/thing", "refs/heads/fix/x") != sha {
		t.Fatal("the repository was not made, or the push is not in it")
	}
	if o, c, ok, _ := x.log.Owner(bg, "github/new/thing", "refs/heads/fix/x"); !ok || o != "sg4" || c != "C1" {
		t.Fatalf("owner %s %s %v", o, c, ok)
	}
	// Made as init makes it: marked, no hook, no receivepack line in its config.
	dir, _ := x.h.Store().Path("github/new/thing")
	cfg, _ := os.ReadFile(filepath.Join(dir, "config"))
	if strings.Contains(strings.ToLower(string(cfg)), "receive") || strings.Contains(string(cfg), "hooksPath") {
		t.Fatalf("our settings were written into the repository:\n%s", cfg)
	}
	if _, err := os.Stat(filepath.Join(dir, "hooks")); err == nil {
		t.Fatal("the repository has a hooks directory")
	}

	// The case rule applies to a repository made by a push.
	out, err = x.push(card, "github/New/thing", "fix/x:refs/heads/fix/x")
	if err == nil || !strings.Contains(out, "cannot be made") {
		t.Fatalf("a case variant of a repository was made: %v\n%s", err, out)
	}
	// By the names on disk: a disk that ignores case would find `New` for `new` by a stat.
	ents, _ := os.ReadDir(filepath.Join(x.h.Store().Root(), "github"))
	for _, e := range ents {
		if e.Name() == "New" {
			t.Fatal("a case variant's directory was made")
		}
	}
}

func TestARepositoryMadeByAPushThatTakesNothingIsTakenAway(t *testing.T) {
	x := newRecv(t)
	x.create.Store(true)
	x.branch("fix/x", "x.txt")
	card := roomCard("sg4", "C1")
	// A bad name is caught before anything is made.
	if code, out := x.rawPush(card, "github/made/late", Update{zeroSHA, "1234567890123456789012345678901234567890", "refs/heads/a..b"}); code != 200 || !strings.Contains(out, "ng ") {
		t.Fatalf("%d %q", code, out)
	}
	if x.h.Store().Exists("github/made/late") {
		t.Fatal("a repository was made for a push that was refused")
	}
	// A push git itself refuses (an object that is not there) makes it, and then it is taken away.
	code, out := x.rawPush(card, "github/made/late", Update{zeroSHA, "1234567890123456789012345678901234567890", "refs/heads/fix/x"})
	_ = code
	_ = out
	if x.h.Store().Exists("github/made/late") {
		t.Fatalf("a repository that took nothing was kept")
	}
	if _, err := os.Stat(filepath.Join(x.h.Store().Root(), "github", "made")); err == nil {
		t.Fatal("the owner directory was left behind")
	}
	if len(x.pushRows()) != 0 {
		t.Fatalf("rows: %+v", x.pushRows())
	}
}

func TestAnOversizePushIsRefusedAndNothingIsKept(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	old := maxPush
	maxPush = 1 << 20
	t.Cleanup(func() { maxPush = old })
	// Spool files go to the temp directory, so give the test its own and look only there.
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	big := bytes.Repeat([]byte("x"), 2<<20)
	// Declared in the header, and chunked.
	req, _ := http.NewRequest(http.MethodPost, x.url(hubRepo)+"/git-receive-pack", bytes.NewReader(big))
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	x.stamp(req, roomCard("sg4", "C1"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared: %d", res.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodPost, x.url(hubRepo)+"/git-receive-pack", io.MultiReader(bytes.NewReader(big)))
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	x.stamp(req, roomCard("sg4", "C1"))
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked: %d", res.StatusCode)
	}
	// And no spool file is left in the temp directory.
	left, _ := filepath.Glob(filepath.Join(tmp, "atrium-push-*"))
	if len(left) != 0 {
		t.Fatalf("spool files left behind: %v", left)
	}
}

func TestAPushWithNoIdentityIsRefusedAndHeadersAreIgnoredOffTheLink(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")

	// No caller at all: the card headers are there and mean nothing.
	nobody := who{card: "C1", chain: "C1"}
	if out, err := x.push(nobody, hubRepo, "fix/x:refs/heads/fix/x"); err == nil {
		t.Fatalf("a push with no identity landed:\n%s", out)
	}
	if code, _ := x.getRefs(nobody, hubRepo, "git-upload-pack"); code != http.StatusForbidden {
		t.Fatalf("a fetch with no identity: %d", code)
	}
	// The operator reach with card headers: the headers are dropped, and it pushes as the operator.
	x.must(x.push(who{caller: "operator", card: "C1", chain: "C1"}, hubRepo, "fix/x:refs/heads/fix/x"))
	rows := x.pushRows()
	if last := rows[len(rows)-1]; last.Room != "" || last.Card != "" {
		t.Fatalf("an operator push took the card headers: %+v", last)
	}
}

func TestARoomsPushWithNoCardIsRefusedWithASentence(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	out, err := x.push(who{caller: "room:sg4"}, hubRepo, "fix/x:refs/heads/fix/x")
	if err == nil || !strings.Contains(out, "named none") {
		t.Fatalf("a room push with no card: %v\n%s", err, out)
	}
	// Past the advertisement too: a request that skips it.
	sha := x.branch("fix/y", "y.txt")
	code, body := x.rawPush(who{caller: "room:sg4"}, hubRepo, Update{zeroSHA, sha, "refs/heads/fix/y"})
	if code != 200 || !strings.Contains(body, "ng refs/heads/fix/y") || !strings.Contains(body, "named none") {
		t.Fatalf("%d %q", code, body)
	}
	// A card id that is not an id.
	for _, id := range []string{"a b", "../x", strings.Repeat("a", 200), "a;b"} {
		if out, err := x.push(who{caller: "room:sg4", card: id, chain: id}, hubRepo, "fix/x:refs/heads/fix/x"); err == nil {
			t.Fatalf("card id %q was taken:\n%s", id, out)
		}
	}
	if len(x.pushRows()) != 1 {
		t.Fatalf("rows: %+v", x.pushRows())
	}
}

func TestAMirrorIsNotServedOrPushedThroughTheStoreRoute(t *testing.T) {
	x := newRecv(t)
	// A bare repository at a store path with no marker, as a git_repos mirror is.
	ref, _ := ParseName("github/m/mirror")
	dir := x.h.Store().dir(ref)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "--bare")
	sha := x.branch("fix/x", "x.txt")

	for _, svc := range []string{"git-upload-pack", "git-receive-pack"} {
		if code, _ := x.getRefs(operator, "github/m/mirror", svc); code != http.StatusNotFound && svc == "git-upload-pack" {
			t.Errorf("fetching a mirror: %d", code)
		}
	}
	if code, _ := x.post(operator, "github/m/mirror", "git-upload-pack", "application/x-git-upload-pack-request", pktLine("want "+sha+"\n")); code != http.StatusNotFound {
		t.Errorf("a fetch POST on a mirror: %d", code)
	}
	x.create.Store(true)
	for _, w := range []who{operator, roomCard("sg4", "C1")} {
		out, err := x.push(w, "github/m/mirror", "fix/x:refs/heads/fix/x")
		if err == nil {
			t.Fatalf("%v pushed into a mirror:\n%s", w, out)
		}
	}
	if refs, _ := Default.Git(bg, dir, "for-each-ref"); strings.TrimSpace(refs) != "" {
		t.Fatalf("a mirror got refs: %s", refs)
	}
}

// NOBODY PUSHES WHAT AN ADOPTED MIRROR KEEPS IN STEP, the operator included: the mirror pass force-fetches the
// checkout over it. A card's work branch lands beside it.
func TestAnAdoptedMirrorTakesWorkBranchesAndNothingThePassOwns(t *testing.T) {
	x := newRecv(t)
	ref, _ := ParseName("github/m/mirror")
	dir := x.h.Store().dir(ref)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "--bare")
	if err := writeMarker(dir, KindAdopted); err != nil {
		t.Fatal(err)
	}
	sha := x.branch("fix/x", "x.txt")
	for _, spec := range []string{"fix/x:refs/heads/claude/main", "fix/x:refs/heads/main", "fix/x:refs/tags/v1",
		"fix/x:refs/rooms/sg4/fix/x"} {
		for name, w := range map[string]who{"a card": roomCard("sg4", "C1"), "the operator": operator} {
			out, err := x.push(w, "github/m/mirror", spec)
			if err == nil || !strings.Contains(out, "is a mirror the hub keeps in step") {
				t.Fatalf("%s pushed %s into an adopted mirror: %v\n%s", name, spec, err, out)
			}
			if refs, _ := Default.Git(bg, dir, "for-each-ref"); strings.TrimSpace(refs) != "" {
				t.Fatalf("an adopted mirror got refs from %s: %s", name, refs)
			}
			if n := len(x.rows()); n != 0 {
				t.Fatalf("%s left rows in the log: %+v", name, x.rows())
			}
		}
	}
	// The advertisement for a push is offered: what lands is decided ref by ref.
	if code, adv := x.getRefs(roomCard("sg4", "C1"), "github/m/mirror", "git-receive-pack"); code != 200 || strings.Contains(adv, "ERR atrium: ") {
		t.Fatalf("the advertisement for a card: %d %q", code, adv)
	}
	// A hand-built push of claude/main that never asked for the advertisement is refused at the push itself.
	zero := strings.Repeat("0", 40)
	code, out := x.post(operator, "github/m/mirror", "git-receive-pack", "application/x-git-receive-pack-request",
		pushBody(zero+" "+sha1a+" refs/heads/claude/main\x00report-status side-band-64k"))
	if code != 200 || !strings.Contains(out, "is a mirror the hub keeps in step") {
		t.Fatalf("a hand-built claude/main push: %d %q", code, out)
	}
	// A card's work branch lands, with its row, and the card owns it.
	if out, err := x.push(roomCard("sg4", "C1"), "github/m/mirror", "fix/x:refs/heads/fix/x"); err != nil {
		t.Fatalf("a card's work branch into an adopted mirror: %v\n%s", err, out)
	}
	if got, _ := Default.Git(bg, dir, "rev-parse", "refs/heads/fix/x"); strings.TrimSpace(got) != sha {
		t.Fatalf("fix/x is %q, want %s", got, sha)
	}
	if rows := x.rows(); len(rows) != 1 || rows[0].Card != "C1" {
		t.Fatalf("rows: %+v", rows)
	}
	// Fetching it works for a room and for the operator.
	for _, w := range []who{roomCard("sg4", "C1"), operator} {
		if code, _ := x.getRefs(w, "github/m/mirror", "git-upload-pack"); code != 200 {
			t.Fatalf("fetching an adopted mirror: %d", code)
		}
	}
}

func TestARoomAndTheOperatorCanFetchTheStore(t *testing.T) {
	x := newRecv(t)
	main := x.seedMain(hubRepo)
	sha := x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	for _, w := range []who{roomCard("m1mini", "C9"), {caller: "room:m1mini"}, operator} {
		dst := filepath.Join(t.TempDir(), "clone")
		x.must(x.run(t.TempDir(), w, "clone", "-q", x.url(hubRepo), dst))
		if got := git(t, dst, "rev-parse", "origin/main"); got != main {
			t.Fatalf("main in the clone: %s", got)
		}
		if got := git(t, dst, "rev-parse", "origin/fix/x"); got != sha {
			t.Fatalf("fix/x in the clone: %s", got)
		}
	}
	if code, _ := x.getRefs(operator, "github/no/such", "git-upload-pack"); code != http.StatusNotFound {
		t.Fatalf("a repository the hub lacks: %d", code)
	}
}

func TestShallowDeepenAndFilterFetchesAreRefusedBeforeGit(t *testing.T) {
	x := newRecv(t)
	main := x.seedMain(hubRepo)
	want := pktLine("want " + main + " multi_ack_detailed side-band-64k ofs-delta\n")
	for name, extra := range map[string][]byte{
		"shallow":        pktLine("shallow " + main + "\n"),
		"deepen":         pktLine("deepen 1\n"),
		"deepen-since":   pktLine("deepen-since 12345\n"),
		"deepen-not":     pktLine("deepen-not refs/heads/main\n"),
		"filter":         pktLine("filter blob:none\n"),
		"the filter cap": nil,
	} {
		body := want
		if name == "the filter cap" {
			body = pktLine("want " + main + " multi_ack_detailed filter\n")
		}
		body = append(append(append([]byte{}, body...), extra...), flushPkt...)
		body = append(body, pktLine("done\n")...)
		code, out := x.post(roomCard("sg4", "C1"), hubRepo, "git-upload-pack", "application/x-git-upload-pack-request", body)
		if code != 200 || !strings.Contains(out, "ERR atrium: ") {
			t.Errorf("%s: %d %q", name, code, out)
		}
	}
	// With real git: a depth-1 and a filtered clone fail, a plain one works.
	for _, args := range [][]string{{"clone", "-q", "--depth", "1"}, {"clone", "-q", "--filter=blob:none"}} {
		dst := filepath.Join(t.TempDir(), "c")
		out, err := x.run(t.TempDir(), operator, append(args, x.url(hubRepo), dst)...)
		if err == nil {
			t.Errorf("git %v worked:\n%s", args, out)
		}
	}
}

func TestProtocolV2IsDroppedAndTheDumbProtocolIsOff(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	for _, svc := range []string{"git-upload-pack", "git-receive-pack"} {
		code, out := x.getRefs(operator, hubRepo, svc, "Git-Protocol", "version=2")
		if code != 200 || strings.Contains(out, "version 2") || !strings.HasPrefix(out[4:], "# service="+svc) {
			t.Errorf("%s with v2: %d %q", svc, code, out)
		}
	}
	for _, p := range []string{"/HEAD", "/info/refs", "/objects/info/packs", "/config", "/atrium-store"} {
		req, _ := http.NewRequest(http.MethodGet, x.url(hubRepo)+p, nil)
		x.stamp(req, operator)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 {
			t.Errorf("%s answered 200: %q", p, b)
		}
	}
	// Not by an encoded path either.
	for _, p := range []string{"/git/hub/github/o/%2e%2e/r.git/info/refs", "/git/hub/github%2fo/r.git/info/refs",
		"/git/hub/github/o/r.git/../r.git/info/refs", "/git/hub/github.com/o/r.git/info/refs", "/git/hub/o/r.git/info/refs"} {
		req, _ := http.NewRequest(http.MethodGet, x.srv.URL+p+"?service=git-upload-pack", nil)
		x.stamp(req, operator)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode == 200 {
			t.Errorf("%s answered 200", p)
		}
	}
}

func TestARepositorysOwnHookAndConfigAreNotUsed(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	dir, _ := x.h.Store().Path(hubRepo)
	proof := filepath.Join(t.TempDir(), "ran")
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	hook := "#!/bin/sh\ntouch " + filepath.ToSlash(proof) + "\nexit 0\n"
	for _, h := range []string{"pre-receive", "update", "post-receive", "post-update", "reference-transaction"} {
		if err := os.WriteFile(filepath.Join(dir, "hooks", h), []byte(hook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A repository that tries to turn its own checks off, and to point git at its own hook path.
	git(t, dir, "config", "receive.denyNonFastForwards", "false")
	git(t, dir, "config", "core.hooksPath", filepath.Join(dir, "hooks"))
	x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	if _, err := os.Stat(proof); err == nil {
		t.Fatal("a hook the repository carried ran")
	}
	// The hub's own pre-receive still refuses a rewrite.
	git(t, x.work, "switch", "-q", "-C", "fix/x", "main")
	commit(t, x.work, "rewrite.txt", "rewrite")
	git(t, x.work, "switch", "-q", "main")
	if out, err := x.push(roomCard("sg4", "C1"), hubRepo, "--force", "fix/x:refs/heads/fix/x"); err == nil {
		t.Fatalf("a rewrite landed in a repository whose config says it may:\n%s", out)
	}
}

// A PUSH THE LOG CANNOT BEGIN IS NOT TAKEN: the row is written before git runs.
func TestAPushTheLogCannotBeginMovesNothing(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	x.log.FailBegin = errors.New("disk full")
	out, err := x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x")
	if err == nil || !strings.Contains(out, "so it was not taken") {
		t.Fatalf("a push the hub could not record landed: %v\n%s", err, out)
	}
	if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != "" {
		t.Fatalf("the ref moved though the log failed: %s", got)
	}
	// A repository the push would have made is not left behind either.
	x.create.Store(true)
	if out, err := x.push(roomCard("sg4", "C1"), "github/o/new", "fix/x:refs/heads/fix/x"); err == nil {
		t.Fatalf("a push to a new repository landed with no log:\n%s", out)
	}
	if _, err := os.Stat(x.h.Store().dir(Ref{Host: "github", Owner: "o", Repo: "new"})); err == nil {
		t.Fatal("a repository was left by a push the log refused")
	}
	x.log.FailBegin = nil
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
}

// With no log at all, nothing is taken.
func TestAHubWithNoPushLogTakesNoPush(t *testing.T) {
	x := newRecv(t, func(h *Hub) { h.PushLog = nil })
	x.makeRepo(hubRepo)
	x.branch("fix/x", "x.txt")
	if out, err := x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"); err == nil {
		t.Fatalf("a hub with no log took a push:\n%s", out)
	}
	if out, err := x.push(operator, hubRepo, "fix/x:refs/heads/fix/x"); err == nil {
		t.Fatalf("a hub with no log took the operator's push:\n%s", out)
	}
}

// A push git took and the log could not settle STAYS OWNED by its pusher, because the row is pending and counts, and
// the next push to the repository settles it from the refs.
func TestAPushTheLogCannotSettleStaysOwnedAndIsSettledByTheNextPush(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	sha := x.branch("fix/x", "x.txt")
	x.log.FailSettle = errors.New("disk full")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != sha {
		t.Fatalf("the push did not land: %s", got)
	}
	if pend, _ := x.log.Pending(bg, hubRepo); len(pend) != 1 {
		t.Fatalf("pending = %+v", pend)
	}
	// The pending row is the owner, and while the log still cannot settle, no other push to the repository goes in.
	if room, card, ok, _ := x.log.Owner(bg, hubRepo, "refs/heads/fix/x"); !ok || room != "sg4" || card != "C1" {
		t.Fatalf("a pending push is not the owner: %q %q %v", room, card, ok)
	}
	other := filepath.Join(t.TempDir(), "other")
	x.must(x.run(t.TempDir(), operator, "clone", "-q", x.url(hubRepo), other))
	git(t, other, "switch", "-q", "fix/x")
	commit(t, other, "o.txt", "o")
	if out, err := x.run(other, roomCard("m1mini", "D2"), "push", x.url(hubRepo), "fix/x:refs/heads/fix/x"); err == nil {
		t.Fatalf("a push went in while the log could not settle:\n%s", out)
	}
	if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != sha {
		t.Fatalf("the branch moved: %s", got)
	}
	// The owner's next push settles the leftovers first.
	x.log.FailSettle = nil
	x.grow("fix/x", "x2.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	if pend, _ := x.log.Pending(bg, ""); len(pend) != 0 {
		t.Fatalf("still pending: %+v", pend)
	}
	if room, card, ok, _ := x.log.Owner(bg, hubRepo, "refs/heads/fix/x"); !ok || room != "sg4" || card != "C1" {
		t.Fatalf("owner %q %q %v", room, card, ok)
	}
}

// A HUB THAT DIED BETWEEN GIT MOVING A REF AND THE LOG KNOWING leaves a pending row. Until it is settled the branch is
// owned (any card could otherwise take it), and settling it from the refs makes it done if the ref moved and drops it
// if it did not, at startup or on the next push.
func TestACrashBetweenGitAndTheLogLeavesTheBranchOwnedUntilStartupSettlesIt(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	sha := x.branch("fix/x", "x.txt")
	x.must(x.push(operator, hubRepo, "fix/x:refs/heads/tmp"))
	dir, _ := x.h.Store().Path(hubRepo)
	zero := strings.Repeat("0", 40)

	// The pushed branch moved and the log was left pending, a second branch's row was pending and git never moved it.
	git(t, dir, "update-ref", "refs/heads/fix/x", sha)
	git(t, dir, "update-ref", "-d", "refs/heads/tmp")
	_, err := x.log.Begin(bg, PushRow{Repo: hubRepo, Ref: "refs/heads/fix/x", Old: zero, New: sha, Room: "sg4", Card: "C1"},
		PushRow{Repo: hubRepo, Ref: "refs/heads/fix/never", Old: zero, New: sha, Room: "sg4", Card: "C1"})
	if err != nil {
		t.Fatal(err)
	}
	// The branch is owned though no row is done, so another card cannot take it.
	other := filepath.Join(t.TempDir(), "other")
	x.must(x.run(t.TempDir(), operator, "clone", "-q", x.url(hubRepo), other))
	git(t, other, "switch", "-q", "fix/x")
	commit(t, other, "o.txt", "o")
	out, err2 := x.run(other, roomCard("m1mini", "D2"), "push", x.url(hubRepo), "fix/x:refs/heads/fix/x")
	if err2 == nil || !strings.Contains(out, "is owned by sg4's C1") {
		t.Fatalf("a branch with a pending row was taken: %v\n%s", err2, out)
	}

	// Startup settles it from the refs.
	if err := x.h.Reconcile(bg); err != nil {
		t.Fatal(err)
	}
	if pend, _ := x.log.Pending(bg, ""); len(pend) != 0 {
		t.Fatalf("still pending: %+v", pend)
	}
	if room, card, ok, _ := x.log.Owner(bg, hubRepo, "refs/heads/fix/x"); !ok || room != "sg4" || card != "C1" {
		t.Fatalf("the moved branch: owner %q %q %v", room, card, ok)
	}
	if _, _, ok, _ := x.log.Owner(bg, hubRepo, "refs/heads/fix/never"); ok {
		t.Fatal("a branch git never moved has an owner")
	}
	// Reconciling again does nothing. A repository that is gone drops what it had pending.
	if err := x.h.Reconcile(bg); err != nil {
		t.Fatal(err)
	}
	if _, err := x.log.Begin(bg, PushRow{Repo: "github/o/gone", Ref: "refs/heads/x", New: sha, Room: "sg4", Card: "C1"}); err != nil {
		t.Fatal(err)
	}
	if err := x.h.Reconcile(bg); err != nil {
		t.Fatal(err)
	}
	if pend, _ := x.log.Pending(bg, ""); len(pend) != 0 {
		t.Fatalf("a missing repository's row is still pending: %+v", pend)
	}
}

// THE FIRST PUSH TO A REPOSITORY settles what was left pending in it, with no startup in between.
func TestTheNextPushToARepositorySettlesWhatACrashLeftPending(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	sha := x.branch("fix/x", "x.txt")
	x.must(x.push(operator, hubRepo, "fix/x:refs/heads/tmp"))
	dir, _ := x.h.Store().Path(hubRepo)
	git(t, dir, "update-ref", "refs/heads/fix/x", sha)
	git(t, dir, "update-ref", "-d", "refs/heads/tmp")
	if _, err := x.log.Begin(bg, PushRow{Repo: hubRepo, Ref: "refs/heads/fix/x", Old: strings.Repeat("0", 40), New: sha, Room: "sg4", Card: "C1"}); err != nil {
		t.Fatal(err)
	}
	x.branch("fix/y", "y.txt")
	x.must(x.push(roomCard("m1mini", "D2"), hubRepo, "fix/y:refs/heads/fix/y"))
	if pend, _ := x.log.Pending(bg, ""); len(pend) != 0 {
		t.Fatalf("still pending after a push to the repository: %+v", pend)
	}
	if room, card, ok, _ := x.log.Owner(bg, hubRepo, "refs/heads/fix/x"); !ok || room != "sg4" || card != "C1" {
		t.Fatalf("owner of the branch the crash left: %q %q %v", room, card, ok)
	}
}

// A TAKEOVER THAT GIT REFUSES RELEASES NOTHING: the release is written with the push row when it lands.
func TestATakeoverGitRefusesReleasesNothing(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	x.setCard("sg4", "C1", CardState{}, ErrCardGone)

	// m1mini's card pushes a branch that is NOT a descendant of fix/x: the hub's rules let the takeover through (the
	// owner is gone) and the hook refuses the non-fast-forward.
	diverged := filepath.Join(t.TempDir(), "d")
	x.must(x.run(t.TempDir(), operator, "clone", "-q", x.url(hubRepo), diverged))
	git(t, diverged, "switch", "-q", "-c", "fix/x2", "main")
	commit(t, diverged, "d.txt", "d")
	git(t, diverged, "branch", "-f", "fix/x", "fix/x2")
	out, err := x.run(diverged, roomCard("m1mini", "D2"), "push", "--force", x.url(hubRepo), "fix/x:refs/heads/fix/x")
	if err == nil {
		t.Fatalf("a non-fast-forward takeover landed:\n%s", out)
	}
	if room, card, ok, _ := x.log.Owner(bg, hubRepo, "refs/heads/fix/x"); !ok || room != "sg4" || card != "C1" {
		t.Fatalf("a refused takeover released the branch: owner %q %q %v", room, card, ok)
	}
	for _, r := range x.rows() {
		if r.Release {
			t.Fatalf("a refused push wrote a release: %+v", r)
		}
	}
	if pend, _ := x.log.Pending(bg, ""); len(pend) != 0 {
		t.Fatalf("pending: %+v", pend)
	}
}

func TestTwoPushesToTheSameNewBranchHaveExactlyOneWinner(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "a.txt")
	// A second, different history for the same name, from another clone.
	other := filepath.Join(t.TempDir(), "other")
	x.must(x.run(t.TempDir(), operator, "clone", "-q", x.url(hubRepo), other))
	git(t, other, "switch", "-q", "-c", "fix/x")
	commit(t, other, "b.txt", "b")

	var wg sync.WaitGroup
	var wins atomic.Int32
	run := func(dir string, w who) {
		defer wg.Done()
		if _, err := x.run(dir, w, "push", x.url(hubRepo), "fix/x:refs/heads/fix/x"); err == nil {
			wins.Add(1)
		}
	}
	wg.Add(2)
	go run(x.work, roomCard("sg4", "C1"))
	go run(other, roomCard("m1mini", "C2"))
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d pushes won", wins.Load())
	}
	rows := x.pushRows()
	if len(rows) != 2 {
		t.Fatalf("rows: %+v", rows)
	}
	o, c, ok, _ := x.log.Owner(bg, hubRepo, "refs/heads/fix/x")
	if !ok || rows[1].Room != o || rows[1].Card != c {
		t.Fatalf("the log and the owner disagree: %+v vs %s %s", rows[1], o, c)
	}
	x.noLeftovers(hubRepo)
}

func TestTheListingTakesOwnersAndTimesFromThePushLog(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	x.branch("fix/y", "y.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	x.must(x.push(roomCard("m1mini", "C2"), hubRepo, "fix/y:refs/heads/fix/y"))
	x.setCard("m1mini", "C2", CardState{}, ErrCardGone)
	if _, err := x.h.ReleaseBranch(bg, hubRepo, "fix/y"); err != nil {
		t.Fatal(err)
	}
	// A row for a ref that no longer exists is ignored.
	_ = x.log.Append(bg, PushRow{Repo: hubRepo, Ref: "refs/heads/gone", Old: zeroSHA, New: "abc", Room: "sg4", Card: "C9"})

	views, err := x.h.Store().View(bg)
	if err != nil || len(views) != 1 {
		t.Fatalf("view: %v %v", views, err)
	}
	v := views[0]
	by := map[string]BranchView{}
	for _, b := range v.Branches {
		by[b.Name] = b
	}
	if len(by) != 2 || by["gone"].Name != "" {
		t.Fatalf("branches: %+v", v.Branches)
	}
	if b := by["fix/x"]; b.Room != "sg4" || b.Card != "C1" || b.Released || b.SHA != x.refOn(hubRepo, "refs/heads/fix/x") {
		t.Fatalf("fix/x: %+v", b)
	}
	if b := by["fix/y"]; b.Room != "m1mini" || b.Card != "C2" || !b.Released {
		t.Fatalf("fix/y: %+v", b)
	}
	// main.at is the operator's last push to main, and not a card's push or the operator's push of a branch.
	rows := x.pushRows()
	if v.Main.At == nil || *v.Main.At != rows[0].At.UTC().Format(time.RFC3339) {
		t.Fatalf("main.at %v, the operator pushed at %v", v.Main.At, rows[0].At)
	}
	// A repository with no operator push shows the commit time, as before.
	x.log.rows = nil
	views, _ = x.h.Store().View(bg)
	if views[0].Main.At == nil {
		t.Fatal("main.at is missing without a push row")
	}
}

// pushBody is a receive-pack request with commands and no pack, which the hub refuses before git is run.
func pushBody(lines ...string) []byte {
	var b []byte
	for _, l := range lines {
		b = append(b, pktLine(l)...)
	}
	return append(b, flushPkt...)
}

const (
	sha1a = "1111111111111111111111111111111111111111"
	sha1b = "2222222222222222222222222222222222222222"
)

// THE REFUSAL IS THE HUB'S SENTENCE, not git's own backup rule that would also have held.
func TestDeleteAndTagRefusalsAreTheHubsSentences(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	git(t, x.work, "tag", "v1", "main")
	for _, c := range []struct {
		who  who
		spec string
		want string
	}{
		{roomCard("sg4", "C1"), ":refs/heads/fix/x", "is not done by a push"},
		{operator, ":refs/heads/fix/x", "is not done by a push"},
		{roomCard("sg4", "C1"), "refs/tags/v1:refs/tags/v1", "a card cannot push a tag"},
	} {
		out, err := x.push(c.who, hubRepo, c.spec)
		if err == nil || !strings.Contains(out, "atrium: ") || !strings.Contains(out, c.want) {
			t.Errorf("%s by %+v: %v\n%s", c.spec, c.who, err, out)
		}
	}
	x.must(x.push(operator, hubRepo, "refs/tags/v1:refs/tags/v1"))
	git(t, x.work, "tag", "-f", "v1", "fix/x")
	out, err := x.push(operator, hubRepo, "--force", "refs/tags/v1:refs/tags/v1")
	if err == nil || !strings.Contains(out, "a tag is not moved") {
		t.Errorf("a moved tag: %v\n%s", err, out)
	}
}

// A push that names a ref twice, a push from a shallow clone, and a card's push into an adopted mirror that
// never asked for the advertisement, all hand-built because git does not send them.
func TestHandBuiltPushesAreRefusedBeforeGit(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	caps := "\x00report-status side-band-64k"
	card := roomCard("sg4", "C1")
	post := func(w who, repo string, lines ...string) string {
		code, out := x.post(w, repo, "git-receive-pack", "application/x-git-receive-pack-request", pushBody(lines...))
		if code != 200 {
			t.Fatalf("%d %s", code, out)
		}
		return out
	}
	zero := strings.Repeat("0", 40)

	out := post(card, hubRepo, zero+" "+sha1a+" refs/heads/fix/d"+caps, zero+" "+sha1b+" refs/heads/fix/d")
	if !strings.Contains(out, "is named twice in this push") {
		t.Errorf("a ref twice: %q", out)
	}
	out = post(card, hubRepo, "shallow "+sha1a, zero+" "+sha1a+" refs/heads/fix/s"+caps)
	if !strings.Contains(out, "shallow clone is not taken") || !strings.Contains(out, "atrium: ") {
		t.Errorf("a shallow push: %q", out)
	}
	if x.refOn(hubRepo, "refs/heads/fix/s") != "" || x.refOn(hubRepo, "refs/heads/fix/d") != "" {
		t.Fatal("a refused push moved a ref")
	}

	ref, _ := ParseName("github/m/mirror")
	dir := x.h.Store().dir(ref)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "--bare")
	if err := writeMarker(dir, KindAdopted); err != nil {
		t.Fatal(err)
	}
	out = post(card, "github/m/mirror", zero+" "+sha1a+" refs/heads/claude/main"+caps)
	if !strings.Contains(out, "is a mirror the hub keeps in step") {
		t.Errorf("a card posting into an adopted mirror: %q", out)
	}
}

// A PUSH THAT SAYS HOW BIG IT IS, AND IS TOO BIG, IS REFUSED WITHOUT READING IT.
func TestADeclaredOversizeBodyIsNotRead(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	old := maxPush
	maxPush = 1 << 20
	t.Cleanup(func() { maxPush = old })
	body := &countingReader{r: bytes.NewReader(make([]byte, 2<<20))}
	req := httptest.NewRequest(http.MethodPost, StorePrefix+hubRepo+".git/git-receive-pack", body)
	req.ContentLength = 2 << 20
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	req = req.WithContext(WithCaller(req.Context(), Caller{Kind: CallerRoom, Room: "sg4"}))
	req.Header.Set(HeaderCard, "C1")
	req.Header.Set(HeaderChain, "C1")
	rec := httptest.NewRecorder()
	x.h.StoreHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || body.n > 1<<16 {
		t.Fatalf("code %d after reading %d bytes", rec.Code, body.n)
	}
}

// WHAT GIT IS GIVEN: no card header, no credential, no protocol version.
func TestGitIsNotGivenTheAtriumHeadersCredentialsOrProtocol(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://hub/git/hub/github/o/r.git/info/refs", nil)
	for k, v := range map[string]string{HeaderCard: "C1", HeaderChain: "C1,C0", "Authorization": "Bearer x",
		"Cookie": "a=b", "Git-Protocol": "version=2", "User-Agent": "git/2"} {
		r.Header.Set(k, v)
	}
	out := forGit(r, "/store/o/r.git", "/info/refs")
	for _, h := range []string{HeaderCard, HeaderChain, "Authorization", "Cookie", "Git-Protocol"} {
		if out.Header.Get(h) != "" {
			t.Errorf("git was given %s", h)
		}
	}
	if out.Header.Get("User-Agent") == "" {
		t.Error("the other headers went too")
	}
	if r.Header.Get(HeaderCard) == "" {
		t.Error("the caller's request was changed")
	}
}

// TWO PUSHES THAT WOULD MAKE `Own/r` AND `own/r2` AT ONCE: one makes it, the other is told why. The store-wide lock is
// what holds the case check and the owner directory together, and the repositories' own locks differ.
func TestTwoCaseTwinRepositoriesMadeAtOnceLeaveOne(t *testing.T) {
	x := newRecv(t)
	x.create.Store(true)
	// Wide enough that, without the lock, both pushes pass the case check before either has made a directory.
	inStoreWideLock = func() { time.Sleep(150 * time.Millisecond) }
	t.Cleanup(func() { inStoreWideLock = nil })
	for i := 0; i < 3; i++ {
		a := fmt.Sprintf("github/Own%d/r", i)
		b := fmt.Sprintf("github/own%d/r2", i)
		var wg sync.WaitGroup
		var wins atomic.Int32
		for j, name := range []string{a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				w := roomCard(fmt.Sprintf("sg%d", j), "C1")
				if _, err := x.run(x.work, w, "push", x.url(name), "main:refs/heads/fix/a"); err == nil {
					wins.Add(1)
				}
			}()
		}
		wg.Wait()
		if wins.Load() != 1 {
			t.Fatalf("round %d: %d pushes made a repository", i, wins.Load())
		}
		entries, _ := os.ReadDir(filepath.Join(x.h.Store().Root(), "github"))
		n := 0
		for _, e := range entries {
			if strings.EqualFold(e.Name(), fmt.Sprintf("own%d", i)) {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("round %d: %d owner directories", i, n)
		}
	}
}

// A FEATURE LIST RIDES ON ANY COMMAND LINE, as git reads it, so a push-options or a sha256 on the second line is as
// refused as on the first.
func TestACapabilityOnAnyCommandLineIsHeldToTheSameChecks(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	zero := strings.Repeat("0", 40)
	card := roomCard("sg4", "C1")
	for name, c := range map[string]struct{ second, want string }{
		"push-options on the second line": {"\x00push-options", "push options are not taken"},
		"sha256 on the second line":       {"\x00object-format=sha256", "sha1"},
	} {
		code, out := x.post(card, hubRepo, "git-receive-pack", "application/x-git-receive-pack-request",
			pushBody(zero+" "+sha1a+" refs/heads/fix/a\x00report-status side-band-64k", zero+" "+sha1b+" refs/heads/fix/b"+c.second))
		if code != 200 || !strings.Contains(out, c.want) {
			t.Errorf("%s: %d %q", name, code, out)
		}
	}
	// Both lines with a list, the refusal still reaches git as a sideband message.
	code, out := x.post(card, hubRepo, "git-receive-pack", "application/x-git-receive-pack-request",
		pushBody(zero+" "+sha1a+" refs/heads/fix/a\x00report-status side-band-64k", zero+" "+sha1b+" refs/heads/fix/b\x00ofs-delta push-options"))
	if code != 200 || !strings.Contains(out, "atrium: ") {
		t.Errorf("both lines: %d %q", code, out)
	}
	if x.refOn(hubRepo, "refs/heads/fix/a") != "" || x.refOn(hubRepo, "refs/heads/fix/b") != "" {
		t.Fatal("a refused push moved a ref")
	}
}

// ONE VALUE EACH. A forwarder that Adds to the card headers instead of setting them has a client's own value first,
// and the hub refuses the request instead of letting the first win.
func TestARepeatedCardHeaderIsRefused(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	for name, hdr := range map[string][][2]string{
		"card twice":        {{HeaderCard, "C1"}, {HeaderCard, "C2"}, {HeaderChain, "C1"}},
		"chain twice":       {{HeaderCard, "C1"}, {HeaderChain, "C1"}, {HeaderChain, "C1,C0"}},
		"card, as a casing": {{HeaderCard, "C1"}, {"x-atrium-card", "C2"}, {HeaderChain, "C1"}},
	} {
		req, _ := http.NewRequest(http.MethodGet, x.url(hubRepo)+"/info/refs?service=git-receive-pack", nil)
		req.Header.Set("X-Test-Caller", "room:sg4")
		for _, h := range hdr {
			req.Header.Add(h[0], h[1])
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if !strings.Contains(string(b), "ERR atrium: ") || !strings.Contains(string(b), "more than once") {
			t.Errorf("%s, advertisement: %q", name, b)
		}
		// And the push itself, which is what counts.
		zero := strings.Repeat("0", 40)
		preq, _ := http.NewRequest(http.MethodPost, x.url(hubRepo)+"/git-receive-pack",
			bytes.NewReader(pushBody(zero+" "+sha1a+" refs/heads/fix/a\x00report-status side-band-64k")))
		preq.Header.Set("Content-Type", "application/x-git-receive-pack-request")
		preq.Header.Set("X-Test-Caller", "room:sg4")
		for _, h := range hdr {
			preq.Header.Add(h[0], h[1])
		}
		pres, err := http.DefaultClient.Do(preq)
		if err != nil {
			t.Fatal(err)
		}
		pb, _ := io.ReadAll(pres.Body)
		pres.Body.Close()
		if !strings.Contains(string(pb), "more than once") {
			t.Errorf("%s, push: %q", name, pb)
		}
	}
	// One of each is fine.
	x.branch("fix/x", "x.txt")
	x.must(x.push(roomCard("sg4", "C1", "C1", "C0"), hubRepo, "fix/x:refs/heads/fix/x"))
}

// A PUSH ASKS AT MOST maxOwnerAsks ROOMS, and the owners it did not ask are treated as unreachable: the branch stays
// owned, and the refusal says why.
func TestAPushAsksOnlyAFewOwnersAndTheRestStayOwned(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	var refs, specs []string
	for i := 0; i < 12; i++ {
		ref := fmt.Sprintf("refs/heads/fix/o%d", i)
		refs = append(refs, ref)
		specs = append(specs, "main:"+ref)
		if err := x.log.Append(bg, PushRow{Repo: hubRepo, Ref: ref, Room: "sg4", Card: fmt.Sprintf("O%d", i)}); err != nil {
			t.Fatal(err)
		}
		x.setCard("sg4", fmt.Sprintf("O%d", i), CardState{}, ErrCardGone)
	}
	out, err := x.push(roomCard("m1mini", "D2"), hubRepo, specs...)
	if err == nil {
		t.Fatalf("a push over owners nobody asked about landed:\n%s", out)
	}
	if got := x.asked.Load(); got != int32(maxOwnerAsks) {
		t.Fatalf("%d owners were asked, at most %d are", got, maxOwnerAsks)
	}
	if !strings.Contains(out, "cannot reach sg4") {
		t.Fatalf("the refusal does not say why:\n%s", out)
	}
	for _, ref := range refs {
		if x.refOn(hubRepo, ref) != "" {
			t.Fatalf("%s landed", ref)
		}
	}
}

// AND FOR AT MOST ownerAskTotal IN ALL, a room that does not answer taking the rest of it.
func TestAPushAsksOwnersForABoundedTimeInAll(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	old := ownerAskTotal
	ownerAskTotal = 300 * time.Millisecond
	t.Cleanup(func() { ownerAskTotal = old })
	var asks atomic.Int32
	x.h.Cards = func(ctx context.Context, room, card string) (CardState, error) {
		asks.Add(1)
		<-ctx.Done()
		return CardState{}, ctx.Err()
	}
	var specs []string
	for i := 0; i < 5; i++ {
		ref := fmt.Sprintf("refs/heads/fix/o%d", i)
		specs = append(specs, "main:"+ref)
		if err := x.log.Append(bg, PushRow{Repo: hubRepo, Ref: ref, Room: "sg4", Card: fmt.Sprintf("O%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	out, err := x.push(roomCard("m1mini", "D2"), hubRepo, specs...)
	if err == nil {
		t.Fatalf("a push landed over owners who never answered:\n%s", out)
	}
	if took := time.Since(start); took > 4*time.Second {
		t.Fatalf("the push took %s to be refused", took)
	}
	if got := asks.Load(); got != 1 {
		t.Fatalf("%d owners were asked after the time was used up", got)
	}
	if !strings.Contains(out, "cannot reach sg4") {
		t.Fatalf("the refusal does not say why:\n%s", out)
	}
}

// THE NAME OF A REF IS CHECKED BEFORE THE REPOSITORY'S LOCK, one process per ref.
func TestRefNamesAreCheckedBeforeTheRepositoryLockIsTaken(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("fix/x", "x.txt")
	l := x.h.lock("store:" + strings.ToLower(hubRepo))
	var calls, held atomic.Int32
	inNameCheck = func() {
		calls.Add(1)
		if !l.TryLock() {
			held.Add(1)
			return
		}
		l.Unlock()
	}
	t.Cleanup(func() { inNameCheck = nil })
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	if calls.Load() == 0 {
		t.Fatal("no ref name was checked")
	}
	if held.Load() != 0 {
		t.Fatal("a ref name was checked with the repository's lock held")
	}
}

type lockProbe struct {
	http.ResponseWriter
	l    *sync.Mutex
	held bool
	n    int
}

func (p *lockProbe) note() {
	p.n++
	if !p.l.TryLock() {
		p.held = true
		return
	}
	p.l.Unlock()
}

func (p *lockProbe) WriteHeader(c int) { p.note(); p.ResponseWriter.WriteHeader(c) }

func (p *lockProbe) Write(b []byte) (int, error) { p.note(); return p.ResponseWriter.Write(b) }

// THE ANSWER IS WRITTEN TO THE CLIENT AFTER THE LOCK IS LET GO, so a client that stops reading holds nothing.
func TestTheAnswerIsWrittenAfterTheRepositoryLockIsReleased(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	l := x.h.lock("store:" + strings.ToLower(hubRepo))
	zero := strings.Repeat("0", 40)
	// A push the hub's rules refuse under the lock (a card moving main), and one that lands.
	body := pushBody(zero + " " + sha1a + " refs/heads/main\x00report-status side-band-64k")
	req := httptest.NewRequest(http.MethodPost, StorePrefix+hubRepo+".git/git-receive-pack", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	req.Header.Set(HeaderCard, "C1")
	req.Header.Set(HeaderChain, "C1")
	req = req.WithContext(WithCaller(req.Context(), Caller{Kind: CallerRoom, Room: "sg4"}))
	probe := &lockProbe{ResponseWriter: httptest.NewRecorder(), l: l}
	x.h.StoreHandler().ServeHTTP(probe, req)
	if probe.n == 0 {
		t.Fatal("nothing was written")
	}
	if probe.held {
		t.Fatal("the answer was written with the repository's lock held")
	}
	if rec := probe.ResponseWriter.(*httptest.ResponseRecorder); !strings.Contains(rec.Body.String(), "only the operator moves main") {
		t.Fatalf("the answer: %q", rec.Body.String())
	}
}

func TestGitsProbeBeforeALargePushIsAnsweredAndDoesNothing(t *testing.T) {
	x := newRecv(t)
	card := roomCard("sg4", "C1")
	x.create.Store(true)

	code, body := x.post(card, "github/new/thing", "git-receive-pack", "application/x-git-receive-pack-request", []byte("0000"))
	if code != http.StatusOK || body != "" {
		t.Fatalf("the probe: %d %q", code, body)
	}
	if len(x.rows()) != 0 || x.h.Store().Exists("github/new/thing") {
		t.Fatalf("the probe wrote a log row or made a repository: %+v", x.rows())
	}
	// A real push with no updates is still refused.
	code, _ = x.post(card, "github/new/thing", "git-receive-pack", "application/x-git-receive-pack-request", []byte("00000000"))
	if code != http.StatusBadRequest {
		t.Fatalf("an empty push: %d", code)
	}
	// And the probe is held to the same caller check.
	code, _ = x.post(who{}, "github/new/thing", "git-receive-pack", "application/x-git-receive-pack-request", []byte("0000"))
	if code == http.StatusOK {
		t.Fatal("a probe with no identity was answered")
	}
}

func TestAPushThatMakesGitProbeLandsThroughTheReceiver(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	sha := x.branch("fix/x", "x.txt")
	out, err := x.run(x.work, roomCard("sg4", "C1"), "-c", "http.postBuffer=1", "push", x.url(hubRepo), "fix/x:refs/heads/fix/x")
	x.must(out, err)
	if got := x.refOn(hubRepo, "refs/heads/fix/x"); got != sha {
		t.Fatalf("fix/x is %q on the hub, want %q", got, sha)
	}
}
