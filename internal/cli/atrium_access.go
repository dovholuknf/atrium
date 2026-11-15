package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dovholuknf/atrium/internal/daemon/publish"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
	linktransport "github.com/dovholuknf/atrium/internal/link/transport"
	"github.com/dovholuknf/atrium/internal/zitijoin"
)

const settingAccessEntries = "access_entries"

// accessEntry is one way in, as the Accessing Atrium screen lists it. No secret is kept here: a ziti identity lives in
// its own file under the hub's directory and a zrok token is never stored.
type accessEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Mode     string `json:"mode,omitempty"`
	Serves   string `json:"serves"`
	Identity string `json:"identity,omitempty"`
	URL      string `json:"url,omitempty"`
	State    string `json:"state"`
	File     string `json:"file,omitempty"`
	// Prompt is what a person has to do at the IdP while the entry reads "signing in". Held in memory only.
	Prompt *accessPrompt `json:"prompt,omitempty"`
}

type accessPrompt struct {
	URL  string `json:"url"`
	Code string `json:"code"`
}

// hubAccess is link.HubAccess over the hub's store, the sign-in guard and the ziti enrol code. This package is the seam
// because link may not import any of them.
type hubAccess struct {
	store *hubstore.Store
	guard *publish.Guard
	dir   string
	mu    sync.Mutex
	// rt starts the entries' listeners. Nil starts nothing, and entries read as saved.
	rt     *accessRuntime
	rm     sync.Mutex
	stops  map[string]func()
	failed map[string]string
	// prompts are the IdP sign-ins in flight, by entry id. join is zitijoin.Join, a field so a test can stand in for the IdP.
	prompts map[string]*accessPrompt
	signing sync.WaitGroup
	join    func(ctx context.Context, p zitijoin.Params, j zitijoin.JoinURL, show func(zitijoin.Prompt)) (string, error)
}

func newHubAccess(store *hubstore.Store, guard *publish.Guard, dir string) *hubAccess {
	return &hubAccess{store: store, guard: guard, dir: dir, stops: map[string]func(){}, failed: map[string]string{},
		prompts: map[string]*accessPrompt{}, join: zitijoin.Join}
}

func (a *hubAccess) entries() ([]accessEntry, error) {
	raw, err := a.store.HubSetting(settingAccessEntries)
	if err != nil || raw == "" {
		return nil, err
	}
	var es []accessEntry
	if err := json.Unmarshal([]byte(raw), &es); err != nil {
		return nil, fmt.Errorf("the saved list of access entries is damaged: %w", err)
	}
	return es, nil
}

func (a *hubAccess) saveEntries(es []accessEntry) error {
	raw, err := json.Marshal(es)
	if err != nil {
		return err
	}
	return a.store.SetHubSetting(settingAccessEntries, string(raw))
}

func (a *hubAccess) View() any {
	es, err := a.entries()
	if es == nil {
		es = []accessEntry{}
	}
	for i := range es {
		es[i].State = a.stateOf(es[i])
		a.rm.Lock()
		es[i].Prompt = a.prompts[es[i].ID]
		a.rm.Unlock()
	}
	waiting := []map[string]string{}
	rooms, rerr := a.store.Rooms()
	for _, r := range rooms {
		if r.State == hubstore.StatePending {
			waiting = append(waiting, map[string]string{"name": r.Name, "via": r.Transport})
		}
	}
	c := a.guard.AuthConfig()
	v := map[string]any{
		"auth": map[string]any{
			"enabled": c.Enabled, "provider": c.Issuer, "client": c.ClientID, "allow": c.Allow,
		},
		"entries": es, "waiting": waiting,
	}
	if err = errors.Join(err, rerr); err != nil {
		v["error"] = err.Error()
	}
	return v
}

func (a *hubAccess) SaveAuth(body []byte) error {
	var in struct {
		Provider string   `json:"provider"`
		Client   string   `json:"client"`
		Allow    []string `json:"allow"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return err
	}
	if in.Allow == nil {
		in.Allow = []string{}
	}
	enabled := strings.TrimSpace(in.Provider) != ""
	mapped, err := json.Marshal(map[string]any{
		"enabled": enabled, "issuer": strings.TrimSpace(in.Provider), "client_id": strings.TrimSpace(in.Client),
		"allow": in.Allow,
	})
	if err != nil {
		return err
	}
	return a.guard.SaveAuthBody(mapped)
}

type accessAdd struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Serves     string `json:"serves"`
	How        string `json:"how"`
	Mode       string `json:"mode"`
	Controller string `json:"controller"`
	Token      string `json:"token"`
	Cert       string `json:"cert"`
	Key        string `json:"key"`
	Ack        bool   `json:"ack"`
	Reach      string `json:"reach"`
	Address    string `json:"address"`
	NoAuthSure bool   `json:"noauthSure"`
}

func (a *hubAccess) Add(ctx context.Context, body []byte) error {
	var in accessAdd
	if err := json.Unmarshal(body, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("an entry needs a name")
	}
	if in.Serves == "" {
		in.Serves = "both"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	es, err := a.entries()
	if err != nil {
		return err
	}
	for _, e := range es {
		if strings.EqualFold(e.Name, in.Name) {
			return fmt.Errorf("there is already an entry called %q", in.Name)
		}
	}
	e := accessEntry{ID: newAccessID(), Name: in.Name, Kind: in.Kind, Mode: in.Mode, Serves: in.Serves, State: "saved"}
	switch in.Kind {
	case "ziti":
		if err := a.enrolZiti(ctx, in, &e); err != nil {
			return err
		}
	case "zrok":
		if e.Mode == "" {
			e.Mode = "private"
		}
		if e.Mode == "public" {
			if err := a.needSignIn(false); err != nil {
				return err
			}
			// The room link is never on a public URL, so a public share carries the board whatever was picked.
			e.Serves = "board"
		}
		if err := a.enableZrok(in.Token); err != nil {
			return err
		}
		if e.Mode == "private" && in.Reach != "" {
			e.URL = "reach: " + in.Reach
		}
	case "address":
		if in.Address = strings.TrimSpace(in.Address); in.Address == "" {
			return errors.New("an address needs the address")
		}
		if e.Mode == "" {
			e.Mode = "mtls"
		}
		if e.Mode == "oidc" {
			if err := a.needSignIn(in.NoAuthSure); err != nil {
				return err
			}
		}
		e.URL = in.Address
	default:
		return fmt.Errorf("no kind of access called %q. it is ziti, zrok or address", in.Kind)
	}
	a.start(&e)
	return a.saveEntries(append(es, e))
}

// enableZrok makes this machine's zrok environment from an account token when it has none. A machine that has one
// keeps it, and the token is used for nothing else and never stored.
func (a *hubAccess) enableZrok(token string) error {
	if a.rt == nil || strings.TrimSpace(token) == "" {
		return nil
	}
	root, err := linktransport.LoadZrokRoot("")
	if err != nil || root.IsEnabled() {
		return err
	}
	host, _ := os.Hostname()
	_, err = linktransport.EnableZrokRoot(root, strings.TrimSpace(token), host, "atrium@"+host)
	return err
}

func (a *hubAccess) needSignIn(noAuthSure bool) error {
	if noAuthSure {
		return nil
	}
	if c := a.guard.AuthConfig(); !c.Enabled || c.Ready() != nil {
		return errors.New("this would put the board in front of anyone with the address and no sign-in is set. " +
			"fill in the sign-in provider first")
	}
	return nil
}

func (a *hubAccess) enrolZiti(ctx context.Context, in accessAdd, e *accessEntry) error {
	p := zitijoin.Params{Controller: strings.TrimSpace(in.Controller), Name: in.Name, Dir: filepath.Join(a.dir, "identities")}
	var path string
	var err error
	switch in.How {
	case "jwt":
		path, err = zitijoin.EnrollJWT(p, in.Token)
	case "import":
		path, err = zitijoin.Import(ctx, p, in.Cert, in.Key)
	case "", "url":
		if in.Mode == "token" {
			if !in.Ack {
				return errors.New("a token identity asks for an IdP sign-in on every start. tick \"I understand\" to go on")
			}
			path, err = zitijoin.EnrollToToken(ctx, p, "")
		} else {
			e.State = "signing in"
			a.signIn(*e, p)
			return nil
		}
	default:
		return fmt.Errorf("no way to join called %q", in.How)
	}
	if err != nil {
		return err
	}
	e.File = path
	e.Identity = filepath.Base(path)
	e.State = "enrolled"
	if in.Mode == "token" {
		e.State = "saved"
	}
	return nil
}

func (a *hubAccess) Revoke(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	es, err := a.entries()
	if err != nil {
		return err
	}
	for i, e := range es {
		if e.ID != id {
			continue
		}
		if err := a.stop(e); err != nil {
			return err
		}
		if e.File != "" {
			if err := os.Remove(e.File); err != nil && !os.IsNotExist(err) {
				return err
			}
			_ = os.Remove(zitijoin.SignInFile(e.File))
		}
		return a.saveEntries(append(es[:i:i], es[i+1:]...))
	}
	return fmt.Errorf("no entry %q: %w", id, link.ErrAccessNotFound)
}

func (a *hubAccess) OpenUp(id, account string) error {
	account = strings.TrimSpace(account)
	if account == "" {
		return errors.New("name the zrok account to let in")
	}
	es, err := a.entries()
	if err != nil {
		return err
	}
	for _, e := range es {
		if e.ID == id {
			if e.Kind != "zrok" || e.Mode != "private" {
				return errors.New("only a private zrok entry can be opened up to another account")
			}
			return errors.New("this hub is not running that share yet, so there is nothing to grant " + account + " on")
		}
	}
	return fmt.Errorf("no entry %q: %w", id, link.ErrAccessNotFound)
}

func (a *hubAccess) Approve(room string) error {
	if _, err := a.store.ByName(room); err != nil {
		return fmt.Errorf("no room %q: %w", room, link.ErrAccessNotFound)
	}
	return a.store.Approve(room)
}

// maxParked bounds the rooms waiting for approval, so a stranger the overlay admits cannot fill the list.
const maxParked = 20

// parkOverlayRoom is conn.Hub.OnOverlayRoom: a name the hub has no record of waits for approval, and a known one is left
// alone.
func parkOverlayRoom(store *hubstore.Store) func(name, over string) {
	return func(name, over string) {
		if _, err := store.ByName(name); err == nil || errors.Is(err, hubstore.ErrHalted) {
			return
		}
		rooms, err := store.Rooms()
		if err != nil {
			return
		}
		n := 0
		for _, r := range rooms {
			if r.State == hubstore.StatePending {
				n++
			}
		}
		if n >= maxParked {
			return
		}
		if _, err := store.AddPending(name, over); err != nil {
			log.Printf("[hub] could not park %q for approval: %v", name, err)
		}
	}
}

func newAccessID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
