package transport

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"

	"github.com/dovholuknf/atrium/internal/zitictx"
	"github.com/openziti/sdk-golang/ziti/edge"
)

func init() { RegisterAccess(zitiAccess{}) }

// zitiAccess is the OpenZiti kind. An entry is one identity on one network, and any number of entries may exist.
// Settings: identity (path to the enrolled .json, which internal/zitijoin writes), service, controller.
type zitiAccess struct{}

func (zitiAccess) Kind() string { return "ziti" }

func (zitiAccess) Fields() []AccessField {
	return []AccessField{
		{Name: "controller", Label: "Controller URL", Required: true},
		{Name: "identity", Label: "Identity file", Required: true},
		{Name: "service", Label: "Service", Required: true},
		{Name: "signer", Label: "Sign-in (ext-jwt-signer)"},
	}
}

func (zitiAccess) Hub(e Entry) (HubAccess, error) {
	z, err := zitiOf(e)
	if err != nil {
		return nil, err
	}
	return &zitiHub{e: e, z: z}, nil
}

func (zitiAccess) Room(e Entry) (RoomAccess, error) {
	z, err := zitiOf(e)
	if err != nil {
		return nil, err
	}
	return zitiRoom{z}, nil
}

func zitiOf(e Entry) (*Ziti, error) {
	id, svc := strings.TrimSpace(e.Settings["identity"]), strings.TrimSpace(e.Settings["service"])
	if id == "" || svc == "" {
		return nil, errors.New("a ziti entry needs an identity and a service")
	}
	return &Ziti{Identity: id, Service: svc}, nil
}

type zitiHub struct {
	e  Entry
	z  *Ziti
	mu sync.Mutex
	ln net.Listener
}

func (h *zitiHub) Listen(context.Context) (net.Listener, error) {
	ln, err := h.z.Listen()
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.ln = ln
	h.mu.Unlock()
	return ln, nil
}

// Join names the network and the service. The room enrolls itself on it: with a one-use JWT when the string carries
// one, or through the device flow at the IdP.
func (h *zitiHub) Join(room string) (string, error) {
	c := strings.TrimSpace(h.e.Settings["controller"])
	if c == "" {
		return "", errors.New("this entry has no controller URL to put in a join string")
	}
	q := url.Values{"room": {room}}
	if s := strings.TrimSpace(h.e.Settings["signer"]); s != "" {
		q.Set("signer", s)
	}
	return "ziti://" + strings.TrimPrefix(c, "https://") + "/" + h.z.Service + "?" + q.Encode(), nil
}

func (*zitiHub) Name(c net.Conn) string {
	if ec, ok := c.(edge.Conn); ok {
		return ec.SourceIdentifier()
	}
	return ""
}

// Key is the dialing identity, which a certificate rotation does not change.
func (h *zitiHub) Key(c net.Conn) string { return h.Name(c) }

func (h *zitiHub) Revoke(context.Context) error {
	h.mu.Lock()
	ln := h.ln
	h.ln = nil
	h.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	zitictx.Close(h.z.Identity)
	return nil
}

func (h *zitiHub) State() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ln == nil {
		return "not bound"
	}
	return "bound " + h.z.Describe()
}

type zitiRoom struct{ z *Ziti }

func (r zitiRoom) Dial(ctx context.Context) (net.Conn, error) { return r.z.Dial(ctx) }
func (r zitiRoom) Describe() string                           { return r.z.Describe() }
func (zitiRoom) Mutual() bool                                 { return false }
