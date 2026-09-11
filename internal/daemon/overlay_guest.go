package daemon

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	zroksdk "github.com/openziti/zrok/v2/sdk/golang/sdk"

	"github.com/dovholuknf/atrium/internal/store"
)

// Lending ONE session to ONE person.
//
// The board over an overlay is already a thing atrium does, and it hands over
// everything: every card, every directory, every permission, the settings, the
// filesystem browser. That is the right answer for reaching your own board
// from your own phone and the wrong answer for giving somebody a link.
//
// This is the other shape. A share that answers for a single card and refuses
// everything else, so the link you send is a link to one terminal and cannot
// be walked back to the rest of the machine.
//
// THE REFUSAL IS AN ALLOWLIST, and it has to stay one. A denylist over an API
// this size is a list of the paths somebody remembered, and the endpoint added
// next week is not on it. Everything here is enumerated, everything else is
// 403, and a new endpoint is invisible to a guest until somebody adds it on
// purpose.

// guestShare is one card, published.
type guestShare struct {
	TaskID string `json:"task_id"`
	// Address is what you send somebody. A public share is a URL with the
	// card's id in the fragment; a private one is the zrok command they run.
	Address string `json:"address"`
	// Mode is public or private, kept because the two are given away
	// differently and the board says so.
	Mode string `json:"mode"`
	// Since is when it started, RFC3339.
	Since string `json:"since"`
	// Token is zrok's share token. It IS the address for a private share, and
	// for a public one it is what releases the share, which is worth having on
	// the board so a leftover can be named.
	Token string `json:"token,omitempty"`
	// Name is the reserved name behind a public share, and empty for a private
	// one. Kept apart from the token because they age differently: the token
	// is replaced every time the share is bound, the name never is. Conflating
	// them is how a restart hands out a new address and calls it the old one.
	Name string `json:"name,omitempty"`
	// Live is whether this address answers right now.
	//
	// False means the share is recorded and its card has no terminal, so the
	// link somebody holds reaches nothing until a runner starts there. It is
	// not an error and it is not something to clean up: the address is still
	// this card's and it comes back on its own.
	Live bool `json:"live"`

	// THERE IS NO READ-ONLY MODE, and there is not going to be one.
	//
	// It existed, defaulted to on, and was a lie about what a share is. Access
	// to the address IS the access: there is no login, the link is the whole
	// credential, and a guest owns their copy of the page. Enforcing "watch
	// only" on the socket was real as far as it went, and what it bought was a
	// checkbox that made handing out a link feel safer than it is.
	//
	// Lending a session is lending the session. If that is not what you meant,
	// do not share it.

	srv *http.Server
	ln  net.Listener
}

// publicNamespace is the namespace a public share asks for a name in.
//
// `public` is the token every zrok instance ships with for its public frontend,
// and `zrok overview` prints it beside the wildcard domain it serves. An
// instance that has renamed it will refuse the share by name, which is an error
// worth reading rather than a silent share with no address.
const publicNamespace = "public"

// guestShares is every card currently lent out, by card id.
type guestShares struct {
	mu  sync.Mutex
	all map[string]*guestShare
}

func (g *guestShares) get(taskID string) *guestShare {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.all[taskID]
}

func (g *guestShares) put(s *guestShare) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.all == nil {
		g.all = map[string]*guestShare{}
	}
	g.all[s.TaskID] = s
}

func (g *guestShares) take(taskID string) *guestShare {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.all[taskID]
	delete(g.all, taskID)
	return s
}

func (g *guestShares) list() []guestShare {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]guestShare, 0, len(g.all))
	for _, s := range g.all {
		out = append(out, guestShare{
			TaskID: s.TaskID, Address: s.Address, Mode: s.Mode,
			Since: s.Since, Token: s.Token,
		})
	}
	// Stable, so the board does not reorder itself between polls.
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

// GuestShares is what the board draws.
//
// LIVE AND RECORDED TOGETHER, because a share that should be up and is not is
// the one worth seeing. The board used to list only what was currently served,
// so a card waiting for its runner to come back looked exactly like a card
// that had never been shared, and the address somebody was already holding was
// invisible.
//
// `live` is what separates them. Everything here is an address somebody may
// have, and `live` says whether it answers right now.
func (d *Daemon) GuestShares() any {
	out := d.guests.list()
	for i := range out {
		out[i].Live = true
	}
	seen := map[string]bool{}
	for _, g := range out {
		seen[g.TaskID] = true
	}

	recs, err := d.st.WantedCardShares()
	if err != nil {
		// The live list is still worth answering with. A board that shows less
		// than everything is better than a board that shows nothing.
		log.Printf("[atrium] could not read the recorded shares: %v", err)
		return out
	}
	for _, rec := range recs {
		if seen[rec.TaskID] {
			continue
		}
		out = append(out, guestShare{
			TaskID: rec.TaskID, Address: rec.Address, Mode: rec.Mode,
			Since: rec.CreatedAt, Name: rec.Name, Live: false,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

// shareStep says how far a share has got, over the event stream.
//
// Creating one is several seconds of somebody else's server, and it used to be
// several seconds of nothing at all: the board sat on a POST and the first
// thing the operator saw was either an address or a paragraph. A 500 from the
// zrok instance therefore arrived as a wall of text about a button that had
// looked inert since it was pressed, with no way to tell how far it had got
// before it gave up.
//
// Best effort by construction, and the POST still carries the answer. A window
// that misses every one of these ends up in the same place a moment later, so
// nothing here is load bearing and none of it is worth an error path.
func (d *Daemon) shareStep(taskID, step, text string) {
	d.ap.Broadcast("share-progress", map[string]any{
		"task_id": taskID, "step": step, "text": text,
	})
}

// shareFailed is the same channel carrying the reason it stopped.
//
// Named separately because the board treats it differently: a step replaces
// the line above it, and this one ends the sequence and puts the buttons back.
func (d *Daemon) shareFailed(taskID string, err error) error {
	d.ap.Broadcast("share-progress", map[string]any{
		"task_id": taskID, "step": "failed", "error": err.Error(),
	})
	return err
}

// shareNameAlphabet is what a generated address is spelled with.
//
// No `l`, `o`, `0` or `1`. The address gets read aloud and typed by hand, and
// a character somebody can transcribe wrongly turns an unguessable link into a
// support question. Thirty two symbols over twelve characters is sixty bits,
// which is not guessable at any rate a public frontend would tolerate.
const shareNameAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// shareNameLen is how many of those. See above.
const shareNameLen = 12

// newShareName invents an address for one lent session.
//
// PREFIXED with `atrium-`, which trades a little privacy for a lot of
// recoverability: the prefix says nothing an attacker can use, since the
// entropy is entirely in the suffix, and it is what makes a leftover share
// identifiable on an account that also holds shares from four other tools.
// The operator who has to clean one up by hand should not have to guess.
func newShareName() (string, error) { return newShareNameOf(shareNameLen) }

// newShareNameOf is the same with the length said out loud.
//
// The board's own share uses a shorter one. It is a name somebody reads off a
// screen and types, and it is not the secret: the board behind it is either
// deliberately public or behind the sign-in in `auth.go`, so length here buys
// tidiness rather than security. A LENT SESSION IS THE OPPOSITE and keeps the
// full twelve, because there the address IS the credential.
func newShareNameOf(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("could not generate an address: %w", err)
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = shareNameAlphabet[int(v)%len(shareNameAlphabet)]
	}
	return "atrium-" + string(out), nil
}

// ShareCard publishes one card over zrok and returns what to hand somebody.
//
// Whoever has the address drives the session. See the note on `guestShare`.
//
// THE ADDRESS IS REUSED when this card has been shared before in the same
// mode. That is the point of storing it: the link somebody was given last
// week still reaches this terminal, so handing it over once is enough. A
// change of mode is a different thing to hand somebody and starts fresh.
func (d *Daemon) ShareCard(taskID, mode string) (any, error) {
	task, err := d.st.Get(taskID)
	if err != nil {
		return nil, err
	}
	if !d.sup.has(taskID) {
		return nil, fmt.Errorf("%s has no terminal to share. atrium only owns the "+
			"terminals it started", task.DisplayTitle())
	}
	if old := d.guests.get(taskID); old != nil {
		return nil, fmt.Errorf("this session is already shared at %s. stop that one first",
			old.Address)
	}

	mode = strings.TrimSpace(mode)
	if mode != "public" && mode != "private" {
		return nil, fmt.Errorf("share mode must be public or private, not %q", mode)
	}

	// The row this card already has, if it has one. A stopped share leaves its
	// row behind holding a name that is still reserved, and reusing it is both
	// the feature and the tidy answer: asking for a second name would leave
	// the first one on the account forever.
	rec, err := d.st.CardShareFor(taskID)
	if err != nil {
		return nil, err
	}
	if rec == nil || rec.Mode != mode {
		rec = &store.CardShare{TaskID: taskID, Kind: "zrok", Mode: mode}
	}
	rec.Wanted = true
	return d.bindCardShare(task.DisplayTitle(), rec)
}

// bindCardShare puts a recorded share up, and is the only place that does.
//
// Every route in ends here: sharing for the first time, sharing again after a
// stop, coming back from a restart, and a runner starting on a card that was
// already lent out. They differ in what they have already decided, not in what
// they do, and the one time they were separate functions the restart path
// forgot to record the new token.
func (d *Daemon) bindCardShare(title string, rec *store.CardShare) (*guestShare, error) {
	taskID, mode := rec.TaskID, rec.Mode

	d.shareStep(taskID, "env", "reading this machine's zrok environment")
	root, err := d.zrokRoot()
	if err != nil {
		return nil, d.shareFailed(taskID, fmt.Errorf("could not read the zrok environment: %w", err))
	}
	if !root.IsEnabled() {
		return nil, d.shareFailed(taskID, fmt.Errorf("this machine has no zrok environment yet. "+
			"enable one under settings, expose the board"))
	}

	req := &zroksdk.ShareRequest{
		BackendMode: zroksdk.ProxyBackendMode,
		ShareMode:   zroksdk.ShareMode(mode),
		// Only used to name the share. Nothing dials it: this process answers
		// the listener itself.
		Target: d.defaultBackend(),
	}
	// A PUBLIC SHARE HAS TO ASK FOR A PUBLIC FRONTEND, or it gets none.
	//
	// Without a name selection the controller creates the share, hands back a
	// token, and returns an EMPTY frontend endpoint list. The share exists, it
	// is genuinely public, and there is no address to reach it on. It reads
	// like the share failed halfway.
	//
	// THE NAME IS OURS AND IT IS RESERVED. Reserving and being guessable are
	// independent, which the first version of this got wrong: it refused to
	// reserve on the grounds that a fixed address would be guessable, and
	// generated a throwaway one instead. `newShareName` produces sixty bits of
	// randomness, so the address is unguessable either way, and reserving it
	// is what makes the link somebody was given last week still work.
	//
	// Three separate facts are needed and `overlay_reserve.go` explains why:
	// the name exists, the name is reserved, and the share asks for it.
	if mode == "public" {
		if rec.Namespace == "" {
			rec.Namespace = publicNamespace
		}
		if rec.Name == "" {
			n, err := newShareName()
			if err != nil {
				return nil, d.shareFailed(taskID, err)
			}
			rec.Name = n
		}
		d.shareStep(taskID, "name", "reserving the address "+rec.Name)
		if _, err := d.ReserveZrokName(rec.Namespace, rec.Name); err != nil {
			return nil, d.shareFailed(taskID, err)
		}
		req.NameSelections = []zroksdk.NameSelection{
			{NamespaceToken: rec.Namespace, Name: rec.Name},
		}
	} else if rec.Token != "" {
		// A PRIVATE SHARE HAS NO NAME, so its token is the durable part.
		//
		// Deleting a private share puts its token back on the shelf, so asking
		// for the same one usually gets it back. Usually, not always: nothing
		// holds it in the meantime and another account can take it. That is
		// why this is a request rather than a guarantee, and why the board is
		// told when a rebind came back with a different one.
		req.PrivateShareToken = rec.Token
	}
	// The slow one, and the one that fails. Everything before this is local.
	d.shareStep(taskID, "create", "asking the zrok instance for a "+mode+" share")
	shr, err := zroksdk.CreateShare(root, req)
	if err != nil {
		return nil, d.shareFailed(taskID, zrokSays("could not share "+title, err))
	}
	d.shareStep(taskID, "listen", "opening the listener that answers it")
	ln, err := zroksdk.NewListener(shr.Token, root)
	if err != nil {
		_ = zroksdk.DeleteShare(root, shr)
		return nil, d.shareFailed(taskID,
			zrokSays("the share was created but nothing could answer it", err))
	}

	// WHAT TO HAND SOMEBODY, and it must not claim to be something else.
	//
	// The card's id goes in the FRAGMENT, which is the board's own way of
	// saying "this window is one terminal". A fragment is never sent to a
	// server, so it costs nothing at the far end and the link opens straight
	// into the terminal rather than onto a board the share would refuse.
	//
	// This used to default to `zrok access private <token>` and only replace it
	// when the SDK returned a frontend endpoint. A public share that came back
	// without one was therefore announced as private, which is the worst
	// direction for that to be wrong in: it reads as "they need zrok" about an
	// address anybody with the link can open.
	//
	// So the mode decides the wording, and a public share with no endpoint is
	// reported as the failure it is rather than described as a private one.
	address := ""
	if len(shr.FrontendEndpoints) > 0 {
		address = strings.TrimRight(shr.FrontendEndpoints[0], "/") + "/#term=" + taskID
	}
	if address == "" {
		if mode == "public" {
			// A public share with nothing to reach it on. The share is KEPT
			// rather than deleted: it exists on the account either way, and one
			// that is listed on the board can be stopped from there, while one
			// deleted behind your back cannot be looked at to work out what
			// happened. The token is in the message for the same reason.
			log.Printf("[atrium] public share %s for %s came back with no frontend endpoint",
				shr.Token, taskID)
			address = "no address. zrok token " + shr.Token
		} else {
			address = "zrok access private " + shr.Token
		}
	}

	g := &guestShare{
		TaskID: taskID, Address: address, Mode: mode,
		Since: time.Now().Format(time.RFC3339), Token: shr.Token,
		Name: rec.Name,
	}
	srv := &http.Server{Handler: d.guestHandler(taskID)}
	g.srv, g.ln = srv, ln
	d.guests.put(g)

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[atrium] the share for %s stopped: %v", taskID, err)
		}
	}()

	// Written down AFTER it is up, so a row never claims an address that was
	// never served. The token changes on every bind and the name does not,
	// which is the whole difference between the two columns.
	rec.Token, rec.Address = shr.Token, address
	rec.BoundAt = time.Now().Format(time.RFC3339)
	if err := d.st.PutCardShare(*rec); err != nil {
		// Not fatal. The share is up and refusing to say so would be worse
		// than a share that has to be stopped by hand after a restart.
		log.Printf("[atrium] could not record the share for %s: %v", taskID, err)
	}

	d.shareStep(taskID, "done", address)
	log.Printf("[atrium] sharing one session (%s) at %s (%s, token %s)",
		taskID, address, mode, shr.Token)
	return g, nil
}

// unbindCardShare takes a share down without giving anything up.
//
// The listener stops and the SHARE is released, because a share nothing is
// answering is a live address that returns errors. The NAME is kept, and the
// row is left `wanted`, so the next start puts the same address back.
//
// This is what a shutdown does. It is deliberately not what stopping does.
func (d *Daemon) unbindCardShare(taskID string) {
	g := d.guests.take(taskID)
	if g == nil {
		return
	}
	if g.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = g.srv.Shutdown(ctx)
	}
	if g.Token != "" {
		if root, err := d.zrokRoot(); err == nil {
			if err := zroksdk.DeleteShare(root, &zroksdk.Share{Token: g.Token}); err != nil {
				log.Printf("[atrium] could not release the share %s: %v", g.Token, err)
			}
		}
	}
}

// StopCardShare takes it back for good.
//
// Everything goes: the listener, the share, the reserved name, and the row. A
// stop is the operator saying this link should not work any more, and a name
// left reserved is a link that would work again the moment anything asked for
// it. This is the only path that burns an address.
func (d *Daemon) StopCardShare(taskID string) error {
	rec, err := d.st.CardShareFor(taskID)
	if err != nil {
		return err
	}
	live := d.guests.get(taskID) != nil
	if !live && rec == nil {
		return fmt.Errorf("that session is not shared")
	}
	d.unbindCardShare(taskID)

	if rec != nil {
		// The reserved name goes back. A name that outlives the share it was
		// reserved for is the thing that quietly fills an account up, and
		// nothing else is ever going to ask for this one.
		if rec.Name != "" {
			if err := d.ReleaseZrokName(rec.Namespace, rec.Name); err != nil {
				log.Printf("[atrium] could not release the name %s: %v", rec.Name, err)
			}
		}
		if err := d.st.ForgetCardShare(taskID); err != nil {
			log.Printf("[atrium] could not forget the share for %s: %v", taskID, err)
		}
	}
	log.Printf("[atrium] stopped sharing %s", taskID)
	return nil
}

// stopAllGuestShares releases every lent session, for shutdown.
//
// UNBINDS, and does not stop. A restart is not the operator withdrawing a
// link, and treating it as one is what made every share die at the moment the
// daemon came back. What each of these leaves behind is a row saying it should
// be up, which `RestoreCardShares` reads on the way in.
func (d *Daemon) stopAllGuestShares() {
	for _, g := range d.guests.list() {
		d.unbindCardShare(g.TaskID)
	}
}

// RestoreCardShares puts back every share that should be up.
//
// Called once the supervisor has whatever runners it is going to have, since a
// share with no terminal behind it is an address that answers nothing. A card
// whose runner comes back later is picked up by `EnsureCardShare` instead.
//
// One at a time and in the foreground of its own goroutine: each one is a call
// to somebody else's API and doing twenty at once is how a controller starts
// refusing. A failure is logged and skipped, never fatal, because a daemon
// that will not start because a share could not be restored is a daemon held
// hostage by an overlay.
func (d *Daemon) RestoreCardShares() {
	recs, err := d.st.WantedCardShares()
	if err != nil {
		log.Printf("[atrium] could not read which sessions were shared: %v", err)
		return
	}
	if len(recs) == 0 {
		return
	}
	go func() {
		for i := range recs {
			rec := recs[i]
			// No terminal, no share. The row stays `wanted`, so this card gets
			// its address back if a runner turns up later.
			if !d.sup.has(rec.TaskID) {
				log.Printf("[atrium] %s was shared but has no terminal yet, so its "+
					"address waits for one", rec.TaskID)
				continue
			}
			title := rec.TaskID
			if t, err := d.st.Get(rec.TaskID); err == nil {
				title = t.DisplayTitle()
			}
			if _, err := d.bindCardShare(title, &rec); err != nil {
				log.Printf("[atrium] could not put %s back on its share: %v", title, err)
				continue
			}
			d.publishTask(rec.TaskID)
		}
	}()
}

// EnsureCardShare puts a card back on its address when its runner arrives.
//
// The other half of `RestoreCardShares`, for the ordinary case where the
// daemon came up before the session did. Silent and cheap when the card was
// never shared, which is almost always, because it is on the path every runner
// takes.
func (d *Daemon) EnsureCardShare(taskID string) {
	rec, err := d.st.CardShareFor(taskID)
	if err != nil || rec == nil || !rec.Wanted {
		return
	}
	if d.guests.get(taskID) != nil {
		return
	}
	title := taskID
	if t, err := d.st.Get(taskID); err == nil {
		title = t.DisplayTitle()
	}
	go func() {
		if _, err := d.bindCardShare(title, rec); err != nil {
			log.Printf("[atrium] could not put %s back on its share: %v", title, err)
			return
		}
		d.publishTask(taskID)
	}()
}

// SweepDeadCardShares releases what is recorded against cards that have gone.
//
// A card can be pruned while its share is recorded, and what is left is a name
// reserved on the account that nothing will ever ask for again. Nobody sees
// those: they are not on the board, because the board draws cards, and the
// card is what went.
//
// Only ever touches names ATRIUM RESERVED and recorded. A sweep that read the
// account and deleted what it did not recognise would eventually delete
// somebody else's share, and this machine's zrok account is not atrium's.
func (d *Daemon) SweepDeadCardShares() {
	recs, err := d.st.StaleCardShares()
	if err != nil {
		log.Printf("[atrium] could not look for shares whose cards have gone: %v", err)
		return
	}
	for _, rec := range recs {
		d.unbindCardShare(rec.TaskID)
		if rec.Name != "" {
			if err := d.ReleaseZrokName(rec.Namespace, rec.Name); err != nil {
				log.Printf("[atrium] could not release the orphaned name %s: %v", rec.Name, err)
				// Left recorded on purpose, so the next sweep tries again.
				// Forgetting it here would lose the only record that this name
				// is atrium's to release.
				continue
			}
		}
		if err := d.st.ForgetCardShare(rec.TaskID); err != nil {
			log.Printf("[atrium] could not forget the orphaned share %s: %v", rec.TaskID, err)
			continue
		}
		log.Printf("[atrium] released %s, whose card is gone", rec.Name)
	}
}

// guestHandler is the whole surface a guest can reach.
//
// Read it as the answer to "what did I just give away". Everything not named
// here is refused, including endpoints that do not exist yet.
func (d *Daemon) guestHandler(taskID string) http.Handler {
	board := d.ap.Handler()
	mine := "/v1/tasks/" + taskID

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path

		// The page itself, and what it loads. Static, identical for everybody,
		// and the board is only a terminal in this window because of a
		// fragment the server never sees.
		//
		// `/board.css` and `/js/` are the board itself, split into files. They
		// are as static as the page that loads them, and a guest refused them
		// gets an unstyled document with no script on it, which is not a
		// smaller thing to give away, just a broken one.
		if r.Method == http.MethodGet &&
			(p == "/" || p == "/index.html" || p == "/sw.js" || p == "/board.css" ||
				strings.HasPrefix(p, "/js/") || strings.HasPrefix(p, "/vendor/")) {
			board.ServeHTTP(w, r)
			return
		}

		// Is atrium there. The page asks this on every reconnect and the answer
		// says nothing about anything.
		if r.Method == http.MethodGet && p == "/v1/health" {
			board.ServeHTTP(w, r)
			return
		}

		// This card, and only this one. An exact match, never a prefix: a
		// prefix over ids would let `/v1/tasks/<id>x` through if anything ever
		// created an id with that shape.
		if r.Method == http.MethodGet && p == mine {
			board.ServeHTTP(w, r)
			return
		}

		// The terminal. The point of the whole exercise.
		if p == mine+"/attach" {
			// THE RUNNER'S TERMINAL, NEVER THE CARD'S SHELL.
			//
			// A card may hold two terminals now, told apart by `?kind=shell`
			// (`attach.go`). Lending a session means lending THAT SESSION: the
			// agent, the conversation, the work. A shell in the card's
			// directory is a general purpose command line on this machine, so
			// a share that reached one would be a share of the machine, which
			// is the line `docs/overlays.md` says atrium does not cross.
			//
			// Refused rather than quietly rewritten to the runner. A guest
			// asking for this is either confused or trying it, and both are
			// better answered than silently redirected.
			if r.URL.Query().Get("kind") != "" {
				http.Error(w, "a shared session is the agent's terminal. "+
					"there is nothing else here.", http.StatusForbidden)
				return
			}
			board.ServeHTTP(w, r)
			return
		}

		// The icon on the card, which is the only decoration the page asks for
		// by card.
		if r.Method == http.MethodGet && p == mine+"/icon" {
			board.ServeHTTP(w, r)
			return
		}

		// EVERYTHING ELSE.
		//
		// Named cases that are refused on purpose, so the next person does not
		// have to work out whether they were forgotten:
		//
		//   /v1/tasks          the whole board. the one thing this exists to
		//                      avoid handing over.
		//   /v1/events         a board-wide stream. every card's title and
		//                      directory goes down it.
		//   /v1/permissions    answering a gate is YOUR decision. a guest
		//                      driving a session hits the gate and you answer
		//                      it, which is the correct division.
		//   /v1/rooms          every other machine reporting in, and the gates
		//                      they are waiting on. The same division, further
		//                      out: a guest holding one terminal has no
		//                      business knowing what else you run, let alone
		//                      answering for it.
		//   /v1/settings       machine settings, including paths.
		//   /v1/browse         the directory picker. Bounded to a root set
		//                      since `browseroots.go`, and still not a guest's
		//                      business: those roots are where your work is.
		//   /v1/tasks/*/files  reading and writing files in the directory.
		//   /v1/tasks/*/scrollback/older
		//                      what this terminal held before the last
		//                      restart. A guest was lent a session, which is
		//                      what is happening now. Everything atrium said
		//                      in that directory yesterday is a different
		//                      offer, and it is not this one.
		//
		// The page tolerates all of these failing: it polls, catches, and
		// carries on with what it has.
		http.Error(w, "this link is one terminal. nothing else here is shared.",
			http.StatusForbidden)
	})
}
