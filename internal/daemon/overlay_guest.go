package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openziti/zrok/v2/environment"
	zroksdk "github.com/openziti/zrok/v2/sdk/golang/sdk"
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
	// Token is zrok's share token. Not shown for a public share, where it is
	// implied by the URL, and it IS the address for a private one.
	Token string `json:"token,omitempty"`
	// Writable is whether the guest can type. Off means they watch.
	Writable bool `json:"writable"`

	srv *http.Server
	ln  net.Listener
}

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
			Since: s.Since, Token: s.Token, Writable: s.Writable,
		})
	}
	// Stable, so the board does not reorder itself between polls.
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

// GuestShares is what the board draws.
func (d *Daemon) GuestShares() any {
	return d.guests.list()
}

// ShareCard publishes one card over zrok and returns what to hand somebody.
//
// `writable` decides whether the guest can type into the terminal or only
// watch it. Read-only is the safer default and is not the useful one, so it is
// asked rather than assumed.
func (d *Daemon) ShareCard(taskID, mode string, writable bool) (any, error) {
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

	root, err := environment.LoadRoot()
	if err != nil {
		return nil, fmt.Errorf("could not read the zrok environment: %w", err)
	}
	if !root.IsEnabled() {
		return nil, fmt.Errorf("this machine has no zrok environment yet. enable one " +
			"under settings, expose the board")
	}

	req := &zroksdk.ShareRequest{
		BackendMode: zroksdk.ProxyBackendMode,
		ShareMode:   zroksdk.ShareMode(mode),
		// Only used to name the share. Nothing dials it: this process answers
		// the listener itself.
		Target: d.defaultBackend(),
	}
	// NO RESERVED NAME, deliberately, and this is the opposite of the choice
	// made for the board.
	//
	// The board's address is one you want to keep: it is yours, you bookmark
	// it, it goes on your phone. A lent session is the other thing entirely.
	// The address IS the credential, since there is no login, so it should be
	// hard to guess, used once, and dead the moment you stop the share.
	// Reserving a name for it would mean handing out the same guessable
	// address every time, forever.
	shr, err := zroksdk.CreateShare(root, req)
	if err != nil {
		return nil, zrokSays("could not share "+task.DisplayTitle(), err)
	}
	ln, err := zroksdk.NewListener(shr.Token, root)
	if err != nil {
		_ = zroksdk.DeleteShare(root, shr)
		return nil, zrokSays("the share was created but nothing could answer it", err)
	}

	// The card's id in the FRAGMENT, which is the board's own way of saying
	// "this window is one terminal". A fragment is never sent to a server, so
	// this costs nothing at the far end and means the link opens straight into
	// the terminal rather than on a board the share would refuse anyway.
	address := "zrok access private " + shr.Token
	if len(shr.FrontendEndpoints) > 0 {
		address = strings.TrimRight(shr.FrontendEndpoints[0], "/") + "/#term=" + taskID
	}

	g := &guestShare{
		TaskID: taskID, Address: address, Mode: mode,
		Since: time.Now().Format(time.RFC3339), Token: shr.Token, Writable: writable,
	}
	srv := &http.Server{Handler: d.guestHandler(taskID, writable)}
	g.srv, g.ln = srv, ln
	d.guests.put(g)

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[atrium] the share for %s stopped: %v", taskID, err)
		}
	}()
	log.Printf("[atrium] sharing one session (%s) at %s, %s", taskID, address,
		map[bool]string{true: "writable", false: "read only"}[writable])
	return g, nil
}

// StopCardShare takes it back.
func (d *Daemon) StopCardShare(taskID string) error {
	g := d.guests.take(taskID)
	if g == nil {
		return fmt.Errorf("that session is not shared")
	}
	if g.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = g.srv.Shutdown(ctx)
	}
	// The share goes with it. Leaving it behind would mean a dead address on
	// the account that somebody has to clean up by hand, and a link that looks
	// live and answers nothing.
	if g.Token != "" {
		if root, err := environment.LoadRoot(); err == nil {
			if err := zroksdk.DeleteShare(root, &zroksdk.Share{Token: g.Token}); err != nil {
				log.Printf("[atrium] could not release the share %s: %v", g.Token, err)
			}
		}
	}
	log.Printf("[atrium] stopped sharing %s", taskID)
	return nil
}

// stopAllGuestShares releases every lent session, for shutdown.
func (d *Daemon) stopAllGuestShares() {
	for _, g := range d.guests.list() {
		if err := d.StopCardShare(g.TaskID); err != nil {
			log.Printf("[atrium] %v", err)
		}
	}
}

// guestHandler is the whole surface a guest can reach.
//
// Read it as the answer to "what did I just give away". Everything not named
// here is refused, including endpoints that do not exist yet.
func (d *Daemon) guestHandler(taskID string, writable bool) http.Handler {
	board := d.ap.Handler()
	mine := "/v1/tasks/" + taskID

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path

		// The page itself, and what it loads. Static, identical for everybody,
		// and the board is only a terminal in this window because of a
		// fragment the server never sees.
		if r.Method == http.MethodGet &&
			(p == "/" || p == "/index.html" || p == "/sw.js" ||
				strings.HasPrefix(p, "/vendor/")) {
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
			if !writable {
				// Read only is enforced HERE rather than in the page, because a
				// guest can edit the page. The socket carries input in one
				// direction and output in the other, so a wrapper that drops
				// what the guest sends is the whole enforcement.
				d.attachReadOnly(w, r, taskID)
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
		//   /v1/settings       machine settings, including paths.
		//   /v1/browse         the filesystem.
		//   /v1/tasks/*/files  reading and writing files in the directory.
		//
		// The page tolerates all of these failing: it polls, catches, and
		// carries on with what it has.
		http.Error(w, "this link is one terminal. nothing else here is shared.",
			http.StatusForbidden)
	})
}
