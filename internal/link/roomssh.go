package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// A room's ssh key and login user, published to the hub and relayed to every room. It rides the roomspec kind, so a
// room's name still comes from its certificate and nothing in the request can name another room:
//
//	POST /_room/ssh        this room's {user, key, port, on}, replacing the last
//	GET  /_room/ssh        the set: every room the hub knows that published, with the login user and key
//	GET  /_hub/sshkeys    the same set for the board and for `atrium ssh config add` on a machine with no room
//
// A PUBLIC KEY AND A LOGIN NAME ONLY. Nothing here is a credential, and the hub never writes to any machine: each room
// pulls the set and decides what goes into its own authorized_keys. A removed room is gone from the set on the next
// pull, because its row goes with it. See internal/hubstore/roomssh.go.

// SSHPublish is what a room says about its own ssh.
type SSHPublish struct {
	User string `json:"user"`
	Key  string `json:"key"`
	Port int    `json:"port"`
	On   bool   `json:"on"`
}

// SSHEntry is one room in the set.
type SSHEntry struct {
	hubstore.RoomSSH
	// Online says the room is attached to this hub right now.
	Online bool `json:"online"`
}

func (p *Proxy) sshSet() ([]SSHEntry, error) {
	st := p.roomSpecs()
	if st == nil {
		return nil, fmt.Errorf("this hub keeps no ssh keys")
	}
	rows, err := st.RoomSSHSet()
	if err != nil {
		return nil, err
	}
	online := map[string]bool{}
	if p.hub != nil {
		for _, a := range p.hub.Rooms() {
			online[a.Name] = true
		}
	}
	out := make([]SSHEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, SSHEntry{RoomSSH: r, Online: online[r.Room]})
	}
	return out, nil
}

// serveRoomSSH is the room's side of the link, as the room the hub named.
func (p *Proxy) serveRoomSSH(w http.ResponseWriter, r *http.Request, room string) {
	switch r.Method {
	case http.MethodPost:
		raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<10))
		var in SSHPublish
		if err != nil || json.Unmarshal(raw, &in) != nil {
			crFail(w, http.StatusBadRequest, "that is not an ssh record")
			return
		}
		if in.Port == 0 {
			in.Port = 22
		}
		if err := p.roomSpecs().PutRoomSSH(room, in.User, in.Key, in.Port, in.On); err != nil {
			specFail(w, err, "ssh record")
			return
		}
		crJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodGet:
		p.writeSSHSet(w)
	default:
		http.NotFound(w, r)
	}
}

func (p *Proxy) writeSSHSet(w http.ResponseWriter) {
	set, err := p.sshSet()
	if err != nil {
		log.Printf("[hub] ssh keys: %v", err)
		crFail(w, http.StatusInternalServerError, "the hub could not read the ssh keys. see the hub's log")
		return
	}
	crJSON(w, http.StatusOK, map[string]any{"keys": set})
}

// serveSSHKeysBoard is GET /_hub/sshkeys, read only.
func (p *Proxy) serveSSHKeysBoard(w http.ResponseWriter, r *http.Request) {
	if p.roomSpecs() == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		crFail(w, http.StatusMethodNotAllowed, "the ssh keys are read here with a GET")
		return
	}
	p.writeSSHSet(w)
}

// ErrNoSSHKeys is what a room is told by a hub that does not keep ssh keys yet.
var ErrNoSSHKeys = errors.New("this hub does not keep ssh keys. update the hub")

// PublishSSH sends this room's login user and public key to the hub over the room link. d is the room's dialer.
func PublishSSH(ctx context.Context, d Dialer, room string, in SSHPublish) error {
	b, _ := json.Marshal(in)
	res, body, err := roomSpecDo(ctx, d, room, http.MethodPost, RoomSpecPrefix+"ssh", b, 1<<16)
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNoSSHKeys
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the hub answered %d: %s", res.StatusCode, specSaid(body))
	}
	return nil
}

// FetchSSHSet asks the hub for every room's published key and login user.
func FetchSSHSet(ctx context.Context, d Dialer, room string) ([]SSHEntry, error) {
	res, body, err := roomSpecDo(ctx, d, room, http.MethodGet, RoomSpecPrefix+"ssh", nil, 1<<20)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusNotFound {
		return nil, ErrNoSSHKeys
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the hub answered %d: %s", res.StatusCode, strings.TrimSpace(specSaid(body)))
	}
	var v struct {
		Keys []SSHEntry `json:"keys"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("the hub's ssh answer is not understood: %w", err)
	}
	return v.Keys, nil
}
