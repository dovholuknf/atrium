//go:build integration

package notify

import (
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// testHub is a Service behind the paths the proxy sends to it, with every call answering for a hub with no rooms.
type testHub struct{ *Service }

func newTestHub() *testHub {
	return &testHub{New(Calls{
		RecordAudit: func(room, kind, detail string) {},
		Rooms:       func() []wire.Attached { return nil },
		Has:         func(string) bool { return false },
		GoWork:      func(fn func()) { go fn() },
	})}
}

func (h *testHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimPrefix(r.URL.Path, "/_hub/")
	switch {
	case sub == "push" || strings.HasPrefix(sub, "push/"):
		h.ServePush(w, r, strings.Trim(strings.TrimPrefix(sub, "push"), "/"))
	case sub == "notify" || sub == "notify/test":
		h.ServeNotify(w, r, sub)
	case sub == "presence":
		h.ServePresence(w, r)
	case sub == "growls" || strings.HasPrefix(sub, "growls/"):
		h.ServeGrowls(w, r, sub)
	case sub == "growl-ladder":
		h.ServeGrowlLadder(w, r)
	default:
		http.NotFound(w, r)
	}
}
