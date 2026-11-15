package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/link"
	"github.com/dovholuknf/atrium/internal/zitijoin"
)

// accessRuntime is what starts and stops the listeners an access entry stands for. The hub's own transports are the
// switches, and a zrok board share is opened here because nothing else keeps one for an entry.
type accessRuntime struct {
	transports interface {
		View() []link.TransportView
		Set(name string, on *bool, service, identity *string) (link.TransportView, error)
	}
	// share puts the board on a zrok share and answers how to undo it and where it is.
	share func(mode string) (stop func(), address string, err error)
}

func zitiItems(serves string) []string {
	switch serves {
	case "board":
		return []string{link.TransportBoardZiti}
	case "link":
		return []string{link.TransportRoomZiti}
	}
	return []string{link.TransportBoardZiti, link.TransportRoomZiti}
}

// start brings an entry's listeners up and records why it could not. A token identity holds no credential, so nothing
// can bind with it until somebody signs in.
func (a *hubAccess) start(e *accessEntry) {
	if a.rt == nil {
		return
	}
	a.rm.Lock()
	defer a.rm.Unlock()
	delete(a.failed, e.ID)
	var errs []string
	switch e.Kind {
	case "ziti":
		if e.File == "" || e.State == "saved" {
			return
		}
		on := true
		for _, item := range zitiItems(e.Serves) {
			if _, err := a.rt.transports.Set(item, &on, nil, &e.File); err != nil {
				errs = append(errs, err.Error())
			}
		}
	case "zrok":
		on := true
		if e.Serves != "board" {
			if _, err := a.rt.transports.Set(link.TransportRoomZrok, &on, nil, nil); err != nil &&
				!strings.Contains(err.Error(), "first transport") {
				errs = append(errs, err.Error())
			}
		}
		if e.Serves != "link" && a.stops[e.ID] == nil && a.rt.share != nil {
			stop, addr, err := a.rt.share(e.Mode)
			if err != nil {
				errs = append(errs, err.Error())
			} else {
				a.stops[e.ID], e.URL = stop, addr
			}
		}
	}
	if len(errs) > 0 {
		a.failed[e.ID] = strings.Join(errs, "; ")
	}
}

// stop takes an entry's listeners down. A ziti item goes off only while it still binds with this entry's identity, so
// revoking an older entry never turns off the one that replaced it.
func (a *hubAccess) stop(e accessEntry) error {
	if a.rt == nil {
		return nil
	}
	a.rm.Lock()
	defer a.rm.Unlock()
	delete(a.failed, e.ID)
	var errs []error
	off := false
	switch e.Kind {
	case "ziti":
		for _, v := range a.rt.transports.View() {
			mine := false
			for _, item := range zitiItems(e.Serves) {
				mine = mine || (v.Name == item && v.On && e.File != "" && v.Identity == e.File)
			}
			if mine {
				if _, err := a.rt.transports.Set(v.Name, &off, nil, nil); err != nil {
					errs = append(errs, err)
				}
			}
		}
	case "zrok":
		if stop := a.stops[e.ID]; stop != nil {
			stop()
			delete(a.stops, e.ID)
		}
	}
	return errors.Join(errs...)
}

// stateOf is what an entry is doing now, read from the listeners it started.
func (a *hubAccess) stateOf(e accessEntry) string {
	a.rm.Lock()
	defer a.rm.Unlock()
	if why := a.failed[e.ID]; why != "" {
		return "failing: " + why
	}
	if a.rt == nil || (e.Kind != "ziti" && e.Kind != "zrok") {
		return e.State
	}
	switch e.Kind {
	case "ziti":
		if e.File == "" || e.State == "saved" {
			return e.State
		}
		got := e.State
		for _, v := range a.rt.transports.View() {
			if v.Identity != e.File || !v.On {
				continue
			}
			switch v.State {
			case "bound":
				return "live"
			case "failing":
				return "failing: " + v.LastError
			default:
				got = "binding"
			}
		}
		return got
	default:
		if a.stops[e.ID] != nil {
			return "live"
		}
		if e.Serves != "board" {
			for _, v := range a.rt.transports.View() {
				if v.Name == link.TransportRoomZrok && v.State == "bound" {
					return "live"
				}
			}
		}
	}
	return e.State
}

// StartAll brings every saved entry's listeners up, for a hub that has just started.
func (a *hubAccess) StartAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	es, err := a.entries()
	if err != nil {
		return
	}
	for i := range es {
		if es[i].State == "signing in" {
			es[i].State = "failed"
			a.rm.Lock()
			a.failed[es[i].ID] = "the hub restarted before the sign-in was finished. revoke it and add it again"
			a.rm.Unlock()
		}
		a.start(&es[i])
	}
	_ = a.saveEntries(es)
}

// signIn finishes a ziti join by URL in the background: the person signs in at the IdP with the prompt the entry shows,
// and the entry is enrolled and started when they have.
func (a *hubAccess) signIn(e accessEntry, p zitijoin.Params) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	a.signing.Add(1)
	go func() {
		defer a.signing.Done()
		defer cancel()
		path, err := a.join(ctx, p, zitijoin.JoinURL{Controller: p.Controller, Room: p.Name}, func(pr zitijoin.Prompt) {
			a.rm.Lock()
			a.prompts[e.ID] = &accessPrompt{URL: pr.URL, Code: pr.Code}
			a.rm.Unlock()
		})
		a.rm.Lock()
		delete(a.prompts, e.ID)
		a.rm.Unlock()
		a.mu.Lock()
		defer a.mu.Unlock()
		es, lerr := a.entries()
		i := -1
		for j := range es {
			if es[j].ID == e.ID {
				i = j
			}
		}
		if i < 0 {
			if err == nil {
				_ = os.Remove(path)
			}
			return
		}
		if err != nil || lerr != nil {
			es[i].State = "failed"
			a.rm.Lock()
			a.failed[e.ID] = errors.Join(err, lerr).Error()
			a.rm.Unlock()
		} else {
			es[i].File, es[i].Identity, es[i].State = path, filepath.Base(path), "enrolled"
			a.start(&es[i])
		}
		_ = a.saveEntries(es)
	}()
}
