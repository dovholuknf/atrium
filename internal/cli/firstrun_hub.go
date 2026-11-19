package cli

import (
	"errors"
	"fmt"
	"os/user"
	"runtime"
	"strings"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
	linktransport "github.com/dovholuknf/atrium/internal/link/transport"
)

// firstRunHub is what the board's first-run panel asks of this hub: this machine's room under a name somebody typed,
// and a join string for another machine.
type firstRunHub struct {
	store *hubstore.Store
	side  *hubSide
	l     roomLaunch
}

func (h firstRunHub) Facts() link.FirstRunFacts {
	who := ""
	if u, err := user.Current(); err == nil {
		who = u.Username
		if i := strings.LastIndex(who, `\`); i >= 0 {
			who = who[i+1:]
		}
	}
	return link.FirstRunFacts{Machine: hostname(), User: who, Room: defaultRoomName(), OS: runtime.GOOS}
}

func (h firstRunHub) LocalRoom(name string) error {
	if board, ok := roomAnswers(roomAddressFile(h.l)); ok {
		return fmt.Errorf("this machine's room already answers at %s. it joins the hub on its own", board)
	}
	keys := linktransport.Keys{Dir: h.l.dir}
	if _, err := keys.Joined(); err != nil {
		if err := mintRoomNamed(h.store, h.side, h.side.kind, keys, name); err != nil {
			return fmt.Errorf("could not make the room %q: %w", name, err)
		}
	}
	return ensureRoom(h.store, h.side, h.side.kind, h.l)
}

func (h firstRunHub) JoinString(name string) (string, error) {
	if h.side == nil || h.side.joinString == nil {
		return "", errors.New("this hub offers no join strings. run `atrium rooms add` on its machine")
	}
	r, err := h.store.ByName(name)
	if err != nil {
		if r, err = h.store.Add(name, hubstore.TransportDirect); err != nil {
			return "", err
		}
	}
	secret, err := h.store.Mint(r.ID)
	if err != nil {
		return "", err
	}
	return h.side.joinString(r.Name, secret)
}
